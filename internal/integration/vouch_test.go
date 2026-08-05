//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// Vouch edge creation and removal are already covered end-to-end by
// TestVouchEdgeCreationAndRemoval in trust_test.go. What was missing, and is
// covered here, is the graph's refusal behaviour: cycles, and the daily limit.

// TestVouchCycleIsRejectedByTheRealGraph closes a loop A -> B -> C and then
// asks C to vouch for A.
//
// Cycle detection is a recursive Cypher query against AGE; a mock cannot tell
// us whether that query actually finds the path, which is the whole risk.
func TestVouchCycleIsRejectedByTheRealGraph(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	svcs := newTestServices(t, pool)

	a := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("cycle-a"), domain.RoleMember, 80.0)
	b := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("cycle-b"), domain.RoleMember, 80.0)
	c := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("cycle-c"), domain.RoleMember, 80.0)

	if _, err := svcs.VouchService.Vouch(ctx, a.ID, b.ID); err != nil {
		t.Fatalf("a vouching for b: %v", err)
	}
	if _, err := svcs.VouchService.Vouch(ctx, b.ID, c.ID); err != nil {
		t.Fatalf("b vouching for c: %v", err)
	}

	// c -> a would close the loop a -> b -> c -> a.
	_, err := svcs.VouchService.Vouch(ctx, c.ID, a.ID)
	if err == nil {
		t.Fatal("closing the vouch loop was allowed; expected it to be rejected as a cycle")
	}
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("error = %v, want it to wrap %v", err, service.ErrValidation)
	}

	// The rejection must leave nothing behind: no edge from c to a.
	vouchers, err := svcs.AGEQuerier.FindVouchersWithDepth(ctx, a.ID, 1)
	if err != nil {
		t.Fatalf("FindVouchersWithDepth() error = %v", err)
	}
	if _, ok := vouchers[c.ID]; ok {
		t.Error("the rejected vouch still created a graph edge")
	}
}

// A direct A -> B -> A loop is the shortest cycle there is, and the one a
// depth-limited traversal is most likely to miss.
func TestVouchImmediateReciprocalIsRejected(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	svcs := newTestServices(t, pool)

	a := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("recip-a"), domain.RoleMember, 80.0)
	b := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("recip-b"), domain.RoleMember, 80.0)

	if _, err := svcs.VouchService.Vouch(ctx, a.ID, b.ID); err != nil {
		t.Fatalf("a vouching for b: %v", err)
	}

	_, err := svcs.VouchService.Vouch(ctx, b.ID, a.ID)
	if err == nil {
		t.Fatal("a reciprocal vouch was allowed; expected it to be rejected as a cycle")
	}
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("error = %v, want it to wrap %v", err, service.ErrValidation)
	}
}

// The daily limit is the service's own rule, counted from local midnight. It
// must hold on its own, with no HTTP rate limiter involved — the limiter is a
// separate, coarser defence and is not what enforces "3 per day".
func TestVouchDailyLimitIsEnforcedByTheServiceItself(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	svcs := newTestServices(t, pool)

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("limit-voucher"), domain.RoleMember, 90.0)

	for i := range 3 {
		vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("limit-vouchee"), domain.RoleMember, 50.0)
		if _, err := svcs.VouchService.Vouch(ctx, voucher.ID, vouchee.ID); err != nil {
			t.Fatalf("vouch %d of 3: %v", i+1, err)
		}
	}

	fourth := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("limit-fourth"), domain.RoleMember, 50.0)
	_, err := svcs.VouchService.Vouch(ctx, voucher.ID, fourth.ID)
	if err == nil {
		t.Fatal("a 4th vouch in one day was allowed; the service must refuse it regardless of any HTTP rate limit")
	}
	if !errors.Is(err, service.ErrValidation) {
		t.Fatalf("error = %v, want it to wrap %v", err, service.ErrValidation)
	}
}

// The handler was written and tested before anything routed to it, so nothing
// proved a vouch could actually be made over HTTP. This drives the real router
// end to end: real guards, real service, real AGE graph.
func TestVouchOverHTTP(t *testing.T) {
	pool := testsupport.TestDB(t)

	voucher := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("voucher"), domain.RoleMember, 80.0)
	vouchee := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vouchee"), domain.RoleMember, 20.0)
	handler := testServer(t, pool, voucher).Handler()

	var vouchID string

	t.Run("a member can vouch for another member", func(t *testing.T) {
		body := fmt.Sprintf(`{"vouchee_id":%q}`, vouchee.ID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("POST /api/v1/vouches is not routed: %s", rec.Body.String())
		}
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var vouch domain.Vouch
		if err := json.NewDecoder(rec.Body).Decode(&vouch); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if vouch.VoucherID != voucher.ID || vouch.VoucheeID != vouchee.ID {
			t.Errorf("vouch = %+v, want %s -> %s", vouch, voucher.ID, vouchee.ID)
		}
		vouchID = vouch.ID
	})

	t.Run("the voucher can revoke it again", func(t *testing.T) {
		if vouchID == "" {
			t.Skip("no vouch was created")
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/vouches/"+vouchID, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("DELETE /api/v1/vouches/{id} is not routed: %s", rec.Body.String())
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
		}
	})
}

// Council approval sits behind the same prefix but a different guard, so a
// member reaching it must be refused rather than routed.
func TestVouchApprovalStillRequiresCouncilOverHTTP(t *testing.T) {
	pool := testsupport.TestDB(t)

	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("plain-member"), domain.RoleMember, 80.0)
	handler := testServer(t, pool, member).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/"+member.ID, bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v1/vouches/approve/{id} is not routed: %s", rec.Body.String())
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d for a member: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
