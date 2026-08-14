package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/httpjson"
	internalkratos "github.com/fireynis/the-bell/internal/kratos"
	kratos "github.com/ory/kratos-client-go"
)

type contextKey int

const (
	userContextKey contextKey = iota
	emailVerifiedContextKey
)

// WithUser stores a domain.User in the request context.
func WithUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// UserFromContext retrieves the domain.User stored by WithUser.
func UserFromContext(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(userContextKey).(*domain.User)
	return u, ok
}

// WithEmailVerified records whether the session identity behind this request
// carries a verified address.
//
// It rides in the context rather than on domain.User because it is a fact about
// the Kratos identity, not about the row in users. A field on domain.User would
// read as false for every user loaded from the database by anything that is not
// this middleware — the role checker, the trust worker, `bell check-roles` —
// and a guard reading it there would be answering a question nobody asked
// Kratos.
func WithEmailVerified(ctx context.Context, verified bool) context.Context {
	return context.WithValue(ctx, emailVerifiedContextKey, verified)
}

// EmailVerifiedFromContext reports the verification state recorded by
// WithEmailVerified. The second return distinguishes "unverified" from "no
// session was resolved at all"; RequireVerifiedEmail treats both as unverified,
// and the distinction exists so a future caller need not guess.
func EmailVerifiedFromContext(ctx context.Context) (bool, bool) {
	verified, ok := ctx.Value(emailVerifiedContextKey).(bool)
	return verified, ok
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
	return internalkratos.DisplayNameFromTraits(identity.GetTraits())
}

// identityEmailVerified reports whether a Kratos identity has at least one
// verified address.
//
// Any verified address counts, rather than only one whose `via` is "email".
// The identity schema is per-deployment and email is the only address type this
// application ever provisions, so a stricter test would buy nothing and could
// lock out a town whose schema words the delivery method differently — the
// failure mode this whole feature has to avoid, since a locked-out resident
// cannot even reach the flow that would fix it.
//
// An identity with no verifiable addresses at all — a schema that declares
// none, or a Kratos version that does not return them — is unverified. That is
// the conservative reading, and it is why the flag defaults to off: a town
// whose schema cannot express verification must never have it silently
// enforced.
func identityEmailVerified(identity *kratos.Identity) bool {
	for _, addr := range identity.GetVerifiableAddresses() {
		if addr.Verified {
			return true
		}
	}
	return false
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

// resolved is what a successful session lookup yielded: the local user, plus
// the identity facts a guard downstream needs and only the session can answer.
type resolved struct {
	user *domain.User
	// emailVerified is the identity's verification state at the moment the
	// session was validated, which is as fresh as it can be — Kratos is
	// consulted on every request.
	emailVerified bool
}

// resolveUser turns a request's session cookie into the local user it names.
//
// It is deliberately decision-free: it reports what happened and lets the
// caller choose the response. KratosAuth rejects, OptionalAuth shrugs — but
// both run the same Kratos call and the same local lookup, so a change to how
// a session is validated cannot fix one and quietly miss the other.
func resolveUser(r *http.Request, kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) (resolved, authOutcome) {
	cookie := r.Header.Get("Cookie")
	if cookie == "" {
		return resolved{}, authNoCookie
	}

	session, _, err := kratosClient.FrontendAPI.ToSession(r.Context()).Cookie(cookie).Execute()
	if err != nil {
		logger.Warn("auth: kratos session validation failed", "error", err)
		return resolved{}, authInvalidSession
	}

	identity := session.GetIdentity()
	kratosID := identity.GetId()

	user, err := finder.FindByKratosID(r.Context(), kratosID, identityDisplayName(&identity))
	if err != nil {
		logger.Error("auth: error looking up user", "kratos_id", kratosID, "error", err)
		return resolved{}, authLookupFailed
	}
	if user == nil {
		logger.Warn("auth: no local user for kratos identity", "kratos_id", kratosID)
		return resolved{}, authNoLocalUser
	}

	return resolved{user: user, emailVerified: identityEmailVerified(&identity)}, authOK
}

// KratosAuth validates the Kratos session cookie and populates the request
// context with the corresponding local user, rejecting the request if it
// cannot.
func KratosAuth(kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, outcome := resolveUser(r, kratosClient, finder, logger)
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

			logger.Debug("auth: authenticated", "user_id", res.user.ID, "role", res.user.Role)
			ctx := WithEmailVerified(WithUser(r.Context(), res.user), res.emailVerified)
			next.ServeHTTP(w, r.WithContext(ctx))
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
			res, outcome := resolveUser(r, kratosClient, finder, logger)
			if outcome != authOK {
				next.ServeHTTP(w, r)
				return
			}

			logger.Debug("auth: optional authentication succeeded", "user_id", res.user.ID, "role", res.user.Role)
			ctx := WithEmailVerified(WithUser(r.Context(), res.user), res.emailVerified)
			next.ServeHTTP(w, r.WithContext(ctx))
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

// RequireVerifiedEmail rejects requests whose session identity has no verified
// address. It is installed only when REQUIRE_VERIFIED_EMAIL is set, and it sits
// alongside RequireActive rather than replacing any part of it: the two answer
// different questions ("is this account in good standing" and "did this person
// prove the address they signed up with") and either one can fail on its own.
//
// The message is distinct from every other 403 on purpose. "forbidden" and
// "account suspended" tell a resident to ask a moderator; this one tells them
// to open their inbox, and the client has to be able to tell those apart
// without inspecting the route.
func RequireVerifiedEmail(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// A context with no recorded state is unverified. The only way to reach
		// this guard without one is a route wired past the auth middleware, and
		// "we never asked Kratos" is not evidence of verification.
		if verified, _ := EmailVerifiedFromContext(r.Context()); !verified {
			writeError(w, http.StatusForbidden, "email not verified")
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
