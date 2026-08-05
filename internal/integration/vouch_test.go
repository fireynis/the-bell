//go:build integration

package integration

import (
	"context"
	"errors"
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
