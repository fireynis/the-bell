package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/httpjson"
	kratos "github.com/ory/kratos-client-go"
)

type contextKey int

const userContextKey contextKey = iota

// WithUser stores a domain.User in the request context.
func WithUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// UserFromContext retrieves the domain.User stored by WithUser.
func UserFromContext(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(userContextKey).(*domain.User)
	return u, ok
}

// UserFinder looks up a local user by their Kratos identity ID, auto-creating
// one if this is the identity's first request.
//
// displayName carries the identity's `name` trait so a user provisioned here
// starts with a name rather than a blank field. It is advisory: the finder
// uses it only when creating, never to overwrite a name the user has since
// edited in-app, and an empty string is a valid argument for callers that have
// no identity to read it from.
type UserFinder interface {
	FindByKratosID(ctx context.Context, kratosID, displayName string) (*domain.User, error)
}

// identityDisplayName reads the `name` trait off a Kratos identity.
//
// Traits arrive as decoded JSON — the SDK types them as interface{} and the
// identity schema is per-deployment — so every step here is a checked
// assertion: a schema without `name`, or with a `name` that is not a string,
// yields "" and a user with no display name, which is exactly what happened
// before this trait was read at all. It is never an auth failure.
func identityDisplayName(identity *kratos.Identity) string {
	traits, ok := identity.GetTraits().(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := traits["name"].(string)
	return strings.TrimSpace(name)
}

// authOutcome says why a session lookup did not yield a local user, so that
// the two middlewares below can share one resolution path and disagree only
// about what to do with the answer.
type authOutcome int

const (
	authOK authOutcome = iota
	authNoCookie
	authInvalidSession
	authLookupFailed
	authNoLocalUser
)

// resolveUser turns a request's session cookie into the local user it names.
//
// It is deliberately decision-free: it reports what happened and lets the
// caller choose the response. KratosAuth rejects, OptionalAuth shrugs — but
// both run the same Kratos call and the same local lookup, so a change to how
// a session is validated cannot fix one and quietly miss the other.
func resolveUser(r *http.Request, kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) (*domain.User, authOutcome) {
	cookie := r.Header.Get("Cookie")
	if cookie == "" {
		return nil, authNoCookie
	}

	session, _, err := kratosClient.FrontendAPI.ToSession(r.Context()).Cookie(cookie).Execute()
	if err != nil {
		logger.Warn("auth: kratos session validation failed", "error", err)
		return nil, authInvalidSession
	}

	identity := session.GetIdentity()
	kratosID := identity.GetId()

	user, err := finder.FindByKratosID(r.Context(), kratosID, identityDisplayName(&identity))
	if err != nil {
		logger.Error("auth: error looking up user", "kratos_id", kratosID, "error", err)
		return nil, authLookupFailed
	}
	if user == nil {
		logger.Warn("auth: no local user for kratos identity", "kratos_id", kratosID)
		return nil, authNoLocalUser
	}

	return user, authOK
}

// KratosAuth validates the Kratos session cookie and populates the request
// context with the corresponding local user, rejecting the request if it
// cannot.
func KratosAuth(kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, outcome := resolveUser(r, kratosClient, finder, logger)
			switch outcome {
			case authNoCookie:
				logger.Warn("auth: no cookie header")
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			case authInvalidSession:
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			case authLookupFailed:
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			case authNoLocalUser:
				writeError(w, http.StatusUnauthorized, "user not found")
				return
			}

			logger.Debug("auth: authenticated", "user_id", user.ID, "role", user.Role)
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// OptionalAuth populates the request context with the signed-in user when the
// request carries a usable session, and otherwise lets it through anonymously.
//
// It exists for routes that are public but personalized: the town feed and a
// single post are readable by anyone, yet the response depends on who is
// asking — which reactions are the caller's own, and whether a removed post is
// theirs to see. Those routes previously ran with no auth middleware at all,
// so the handler never saw a user even when one was signed in.
//
// It fails open by design. A missing, malformed, or expired cookie means "no
// user", never 401 — rejecting would make a public page unreadable to the
// logged-out visitors it exists for. A failed local lookup is treated the same
// way: the alternative is returning 500 for the whole feed because we could
// not name the reader, which trades a degraded response for no response. The
// cost is that a moderator sees the anonymous view during a database blip;
// that is a visibility degradation, not an escalation, and every route where
// identity is load-bearing uses KratosAuth instead.
func OptionalAuth(kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, outcome := resolveUser(r, kratosClient, finder, logger)
			if outcome != authOK {
				next.ServeHTTP(w, r)
				return
			}

			logger.Debug("auth: optional authentication succeeded", "user_id", user.ID, "role", user.Role)
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// roleRank maps roles to an integer rank for comparison.
var roleRank = map[domain.Role]int{
	domain.RoleBanned:    0,
	domain.RolePending:   1,
	domain.RoleMember:    2,
	domain.RoleModerator: 3,
	domain.RoleCouncil:   4,
}

// RequireActive rejects requests from inactive (suspended) users.
func RequireActive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if !user.IsActive {
			writeError(w, http.StatusForbidden, "account suspended")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects requests from users whose role ranks below minRole.
func RequireRole(minRole domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			userLevel := roleRank[user.Role] // unknown roles get 0
			required := roleRank[minRole]

			if userLevel < required {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	httpjson.WriteError(w, status, msg)
}
