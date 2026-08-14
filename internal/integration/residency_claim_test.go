//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// A residency claim travels from the resident who writes it to the council that
// reviews it and stops there. These tests follow that path through the real
// routes and check both halves: that the council can read it, and that nobody
// else can.

const homeAddress = "12 Mill Lane, behind the churchyard"

func putResidencyClaim(t *testing.T, h http.Handler, claim string) *httptest.ResponseRecorder {
	t.Helper()

	body := mustJSON(t, map[string]any{"claim": claim})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/residency-claim", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// storedClaim reads the columns straight from Postgres. The claim appears in
// exactly one response shape, so the database is the only place to check that a
// write landed.
func storedClaim(t *testing.T, pool *pgxpool.Pool, userID string) (claim string, updated bool) {
	t.Helper()

	var stamp *string
	if err := pool.QueryRow(context.Background(),
		`SELECT residency_claim, residency_claim_updated_at::text FROM users WHERE id = $1`, userID,
	).Scan(&claim, &stamp); err != nil {
		t.Fatalf("reading residency claim: %v", err)
	}
	return claim, stamp != nil
}

// reloadUser reads a user back out of the database, the way the auth middleware
// does on every request. Tests that write through one request and read through
// the next need it, because the mock auth in this harness holds one fixed user
// rather than resolving a fresh one per request.
func reloadUser(t *testing.T, pool *pgxpool.Pool, userID string) *domain.User {
	t.Helper()

	user, err := postgres.NewUserRepo(postgres.New(pool)).GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reloading user %s: %v", userID, err)
	}
	return user
}

// enterBootstrapMode puts the town back where the approval queue is reachable.
// The queue is council-only AND bootstrap-only, so a claim cannot be reviewed
// outside it.
func enterBootstrapMode(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`UPDATE town_config SET value = 'true' WHERE key = 'bootstrap_mode'`); err != nil {
		t.Fatalf("enabling bootstrap mode: %v", err)
	}
}

// The path the feature exists for: a pending resident says where they live, and
// the council reviewing them sees it.
func TestResidencyClaim_ReachesTheCouncilsApprovalQueue(t *testing.T) {
	pool := testsupport.TestDB(t)
	enterBootstrapMode(t, pool)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("claim-resident"), domain.RolePending, 50)
	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("claim-council"), domain.RoleCouncil, 100)

	residentServer := testServer(t, pool, resident).Handler()
	councilServer := testServer(t, pool, council).Handler()

	// A pending resident is active, which is what makes their application
	// reviewable — and what lets them past RequireActive here.
	rec := putResidencyClaim(t, residentServer, homeAddress)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want it empty", rec.Body.String())
	}

	stored, stamped := storedClaim(t, pool, resident.ID)
	if stored != homeAddress {
		t.Errorf("stored claim = %q, want %q", stored, homeAddress)
	}
	if !stamped {
		t.Error("residency_claim_updated_at was not stamped")
	}

	queueRec := httptest.NewRecorder()
	councilServer.ServeHTTP(queueRec, httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil))
	if queueRec.Code != http.StatusOK {
		t.Fatalf("reading the queue: status %d: %s", queueRec.Code, queueRec.Body.String())
	}

	var queue struct {
		Users []struct {
			ID             string `json:"id"`
			ResidencyClaim string `json:"residency_claim"`
		} `json:"users"`
	}
	if err := json.Unmarshal(queueRec.Body.Bytes(), &queue); err != nil {
		t.Fatalf("decoding the queue: %v; body: %s", err, queueRec.Body.String())
	}

	var found bool
	for _, u := range queue.Users {
		if u.ID != resident.ID {
			continue
		}
		found = true
		if u.ResidencyClaim != homeAddress {
			t.Errorf("the queue shows %q, want %q", u.ResidencyClaim, homeAddress)
		}
	}
	if !found {
		t.Fatalf("the pending resident is not in the queue: %s", queueRec.Body.String())
	}
}

// And nowhere a third party can read it. The directory is readable by every
// signed-in resident including a pending one, and a profile by anyone at all,
// so a claim leaking into either would publish a neighbour's home address to
// the whole town.
//
// The resident's OWN profile is the deliberate exception and is covered by the
// test below this one.
func TestResidencyClaim_IsNotPublishedToTheTown(t *testing.T) {
	pool := testsupport.TestDB(t)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("leak-resident"), domain.RoleMember, 60)
	neighbour := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("leak-neighbour"), domain.RoleMember, 60)

	residentServer := testServer(t, pool, resident).Handler()
	neighbourServer := testServer(t, pool, neighbour).Handler()

	if rec := putResidencyClaim(t, residentServer, homeAddress); rec.Code != http.StatusNoContent {
		t.Fatalf("setting the claim: status %d: %s", rec.Code, rec.Body.String())
	}

	endpoints := []struct {
		name string
		path string
		h    http.Handler
	}{
		{"the member directory", "/api/v1/users", neighbourServer},
		{"a neighbour's profile", "/api/v1/users/" + resident.ID, neighbourServer},
		{"their posts", "/api/v1/users/" + resident.ID + "/posts", neighbourServer},
		{"their vouches", "/api/v1/users/" + resident.ID + "/vouches", neighbourServer},
	}

	for _, e := range endpoints {
		t.Run(e.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, e.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, homeAddress) {
				t.Errorf("%s published a resident's home address: %s", e.name, body)
			}
			if strings.Contains(body, "residency_claim") {
				t.Errorf("%s published the residency_claim key: %s", e.name, body)
			}
		})
	}
}

// Withdrawing a claim has to be as easy as making one, and it must actually
// clear the column rather than leaving the last version behind.
func TestResidencyClaim_CanBeCleared(t *testing.T) {
	pool := testsupport.TestDB(t)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("clear-resident"), domain.RolePending, 50)
	h := testServer(t, pool, resident).Handler()

	if rec := putResidencyClaim(t, h, homeAddress); rec.Code != http.StatusNoContent {
		t.Fatalf("setting the claim: status %d", rec.Code)
	}
	if rec := putResidencyClaim(t, h, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("clearing the claim: status %d: %s", rec.Code, rec.Body.String())
	}

	stored, stamped := storedClaim(t, pool, resident.ID)
	if stored != "" {
		t.Errorf("claim = %q after clearing, want empty", stored)
	}
	// A withdrawn claim is a different fact from one never made, which is what
	// the timestamp is for: it survives the clear.
	if !stamped {
		t.Error("clearing the claim erased the timestamp; a withdrawal is still an event")
	}
}

// Whitespace is trimmed, and a claim past the limit is refused with a reason
// rather than silently truncated.
func TestResidencyClaim_TrimsAndBoundsTheClaim(t *testing.T) {
	pool := testsupport.TestDB(t)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("bounds-resident"), domain.RolePending, 50)
	h := testServer(t, pool, resident).Handler()

	if rec := putResidencyClaim(t, h, "   "+homeAddress+"  \n"); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if stored, _ := storedClaim(t, pool, resident.ID); stored != homeAddress {
		t.Errorf("stored %q, want it trimmed to %q", stored, homeAddress)
	}

	rec := putResidencyClaim(t, h, strings.Repeat("a", 301))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an over-long claim returned %d, want 400", rec.Code)
	}
	if stored, _ := storedClaim(t, pool, resident.ID); stored != homeAddress {
		t.Errorf("a rejected claim overwrote the stored one: %q", stored)
	}
}

// A banned account has no application in front of the council, and RequireActive
// is what says so.
func TestResidencyClaim_BannedResidentIsRefused(t *testing.T) {
	pool := testsupport.TestDB(t)

	banned := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("banned-resident"), domain.RoleBanned, 0)
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET is_active = FALSE WHERE id = $1`, banned.ID); err != nil {
		t.Fatalf("deactivating the account: %v", err)
	}
	banned.IsActive = false

	h := testServer(t, pool, banned).Handler()

	rec := putResidencyClaim(t, h, homeAddress)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if stored, _ := storedClaim(t, pool, banned.ID); stored != "" {
		t.Errorf("a refused request still wrote %q", stored)
	}
}

// A resident reads their own claim back from all three self views, which is
// what lets the field that collects it prefill in a later session. They wrote
// it, so there is no disclosure here — and without it, changing one word of an
// address means retyping the whole thing into a box that looks like it lost the
// answer.
//
// GET /v1/me and GET /v1/users/me are separate routes served by the same
// handler; both are exercised because the routing is what the frontend depends
// on, not the handler.
func TestResidencyClaim_ComesBackOnTheResidentsOwnProfile(t *testing.T) {
	pool := testsupport.TestDB(t)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("prefill-resident"), domain.RolePending, 50)

	if rec := putResidencyClaim(t, testServer(t, pool, resident).Handler(), homeAddress); rec.Code != http.StatusNoContent {
		t.Fatalf("setting the claim: status %d: %s", rec.Code, rec.Body.String())
	}

	// The reads go through a server built over a freshly-read user, because
	// that is what the next request in production sees: middleware.KratosAuth
	// calls FindByKratosID on every request, so the user in the context is
	// re-read from the database each time. This harness's mock auth injects one
	// fixed value captured at construction, so reusing the server above would
	// assert against a user frozen before the claim was written — a property of
	// the fake, not of the endpoint.
	h := testServer(t, pool, reloadUser(t, pool, resident.ID)).Handler()

	for _, path := range []string{"/api/v1/me", "/api/v1/users/me"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}

			var body struct {
				ResidencyClaim string `json:"residency_claim"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding response: %v; body: %s", err, rec.Body.String())
			}
			if body.ResidencyClaim != homeAddress {
				t.Errorf("residency_claim = %q, want %q", body.ResidencyClaim, homeAddress)
			}
		})
	}

	// Saving the profile must not hand back a response whose residency field
	// looks empty — the write does not touch the column, and the response has
	// to show that.
	t.Run("PUT /api/v1/users/me", func(t *testing.T) {
		body := mustJSON(t, map[string]any{"display_name": "Ada", "bio": "keeps bees", "avatar_url": ""})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), homeAddress) {
			t.Errorf("the updated profile dropped the claim: %s", rec.Body.String())
		}
		// And the profile write really did leave the column alone.
		if stored, _ := storedClaim(t, pool, resident.ID); stored != homeAddress {
			t.Errorf("stored claim = %q after a profile update, want it untouched", stored)
		}
	})
}

// A resident who has said nothing gets the key with an empty string rather than
// no key, so a client prefilling the box has one case to handle instead of two.
func TestResidencyClaim_OwnProfileCarriesTheKeyEvenWhenUnset(t *testing.T) {
	pool := testsupport.TestDB(t)

	resident := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("unset-resident"), domain.RolePending, 50)
	h := testServer(t, pool, resident).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"residency_claim":""`) {
		t.Errorf("body = %s, want an explicit empty residency_claim", rec.Body.String())
	}
}
