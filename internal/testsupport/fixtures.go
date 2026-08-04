package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultTrustScore is the score a newly created user starts with; callers
// asking for it need no update.
const defaultTrustScore = 50.0

// TestUser creates a user directly in the database with the given role and
// trust score.
func TestUser(t *testing.T, pool *pgxpool.Pool, kratosID string, role domain.Role, trust float64) *domain.User {
	t.Helper()
	ctx := context.Background()

	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	svc := service.NewUserService(userRepo, time.Now)

	user, err := svc.FindOrCreate(ctx, kratosID)
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}

	if role != domain.RolePending {
		if err := userRepo.UpdateUserRole(ctx, user.ID, role); err != nil {
			t.Fatalf("updating user role: %v", err)
		}
		user.Role = role
	}

	if trust != defaultTrustScore {
		if err := userRepo.UpdateUserTrustScore(ctx, user.ID, trust); err != nil {
			t.Fatalf("updating user trust score: %v", err)
		}
		user.TrustScore = trust
	}

	return user
}
