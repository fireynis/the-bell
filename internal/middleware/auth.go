package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/fireynis/the-bell/internal/domain"
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

// UserFinder looks up a local user by their Kratos identity ID.
type UserFinder interface {
	FindByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
}

// KratosAuth validates the Kratos session cookie and populates the request
// context with the corresponding local user.
func KratosAuth(kratosClient *kratos.APIClient, finder UserFinder, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie := r.Header.Get("Cookie")
			if cookie == "" {
				logger.Warn("auth: no cookie header")
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			session, _, err := kratosClient.FrontendAPI.ToSession(r.Context()).Cookie(cookie).Execute()
			if err != nil {
				logger.Warn("auth: kratos session validation failed", "error", err)
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			identity := session.GetIdentity()
			kratosID := identity.GetId()

			user, err := finder.FindByKratosID(r.Context(), kratosID)
			if err != nil {
				logger.Error("auth: error looking up user", "kratos_id", kratosID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if user == nil {
				logger.Warn("auth: no local user for kratos identity", "kratos_id", kratosID)
				writeError(w, http.StatusUnauthorized, "user not found")
				return
			}

			logger.Debug("auth: authenticated", "user_id", user.ID, "role", user.Role)
			ctx := WithUser(r.Context(), user)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
