//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

// TestTrustPenaltyPropagation validates end-to-end trust penalty propagation
// through the real Apache AGE graph. This is the most important integration
// test because it exercises:
//   - User creation in both relational tables and the graph
//   - Vouch edge creation in AGE
//   - Graph traversal for penalty propagation (FindVouchersWithDepth)
//   - Penalty record creation with correct hop depths and decay amounts
func TestTrustPenaltyPropagation(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	svcs := newTestServices(pool)

	// Build a vouch chain: council -> mod -> memberA -> memberB
	//
	// council vouches for mod
	// mod vouches for memberA
	// memberA vouches for memberB
	//
	// When we take a moderation action (warn, severity 2) against memberB,
	// penalties should propagate upstream through the vouch graph.
	council := testUser(t, pool, uniqueKratosID("council"), domain.RoleCouncil, 100.0)
	mod := testUser(t, pool, uniqueKratosID("mod"), domain.RoleModerator, 90.0)
	memberA := testUser(t, pool, uniqueKratosID("memberA"), domain.RoleMember, 80.0)
	memberB := testUser(t, pool, uniqueKratosID("memberB"), domain.RoleMember, 75.0)

	// Create vouch chain edges in the AGE graph.
	// council -> mod
	if _, err := svcs.VouchService.Vouch(ctx, council.ID, mod.ID); err != nil {
		t.Fatalf("council vouching for mod: %v", err)
	}
	// mod -> memberA
	if _, err := svcs.VouchService.Vouch(ctx, mod.ID, memberA.ID); err != nil {
		t.Fatalf("mod vouching for memberA: %v", err)
	}
	// memberA -> memberB (memberA trust is 80, above VouchingThreshold 60)
	if _, err := svcs.VouchService.Vouch(ctx, memberA.ID, memberB.ID); err != nil {
		t.Fatalf("memberA vouching for memberB: %v", err)
	}

	t.Run("severity 2 warn propagates 1 hop", func(t *testing.T) {
		// severity 2 (moderate warn): propagation depth = 1 hop
		// Direct penalty on memberB: 10 points
		// Propagated to memberA (1 hop): 10 * 0.70 = 7.0 points
		// mod should NOT be affected (2 hops, but max depth is 1)
		result, err := svcs.ModerationActionService.TakeAction(
			ctx,
			mod.ID,       // moderator
			memberB.ID,   // target
			domain.ActionWarn,
			2,            // severity
			"test warn",
			nil,          // no duration for warnings
		)
		if err != nil {
			t.Fatalf("taking moderation action: %v", err)
		}

		if result.Action == nil {
			t.Fatal("expected action to be created")
		}

		// We expect 2 penalties: 1 direct (memberB) + 1 propagated (memberA)
		if len(result.Penalties) != 2 {
			t.Fatalf("expected 2 penalties, got %d", len(result.Penalties))
		}

		// Verify direct penalty on memberB.
		var directFound, propagatedFound bool
		for _, p := range result.Penalties {
			if p.UserID == memberB.ID {
				directFound = true
				if p.HopDepth != 0 {
					t.Errorf("direct penalty hop_depth: expected 0, got %d", p.HopDepth)
				}
				if p.PenaltyAmount != 10.0 {
					t.Errorf("direct penalty amount: expected 10.0, got %.2f", p.PenaltyAmount)
				}
			}
			if p.UserID == memberA.ID {
				propagatedFound = true
				if p.HopDepth != 1 {
					t.Errorf("propagated penalty hop_depth: expected 1, got %d", p.HopDepth)
				}
				expectedAmount := 10.0 * 0.70 // severity 2 decay rate
				if p.PenaltyAmount != expectedAmount {
					t.Errorf("propagated penalty amount: expected %.2f, got %.2f", expectedAmount, p.PenaltyAmount)
				}
			}
		}

		if !directFound {
			t.Error("direct penalty on memberB not found")
		}
		if !propagatedFound {
			t.Error("propagated penalty on memberA not found")
		}

		// Verify mod was NOT penalized (too far away for severity 2).
		for _, p := range result.Penalties {
			if p.UserID == mod.ID {
				t.Error("mod should not have received a penalty (beyond 1-hop propagation depth)")
			}
		}
	})

	t.Run("severity 5 ban propagates 3 hops", func(t *testing.T) {
		// Create a fresh chain for the ban test to avoid interference.
		banTarget := testUser(t, pool, uniqueKratosID("banTarget"), domain.RoleMember, 75.0)
		voucher1 := testUser(t, pool, uniqueKratosID("voucher1"), domain.RoleMember, 80.0)
		voucher2 := testUser(t, pool, uniqueKratosID("voucher2"), domain.RoleMember, 80.0)
		voucher3 := testUser(t, pool, uniqueKratosID("voucher3"), domain.RoleMember, 80.0)
		voucher4 := testUser(t, pool, uniqueKratosID("voucher4"), domain.RoleMember, 80.0)

		// Chain: voucher4 -> voucher3 -> voucher2 -> voucher1 -> banTarget
		if _, err := svcs.VouchService.Vouch(ctx, voucher1.ID, banTarget.ID); err != nil {
			t.Fatalf("voucher1 vouching for banTarget: %v", err)
		}
		if _, err := svcs.VouchService.Vouch(ctx, voucher2.ID, voucher1.ID); err != nil {
			t.Fatalf("voucher2 vouching for voucher1: %v", err)
		}
		if _, err := svcs.VouchService.Vouch(ctx, voucher3.ID, voucher2.ID); err != nil {
			t.Fatalf("voucher3 vouching for voucher2: %v", err)
		}
		if _, err := svcs.VouchService.Vouch(ctx, voucher4.ID, voucher3.ID); err != nil {
			t.Fatalf("voucher4 vouching for voucher3: %v", err)
		}

		// Ban (severity 5): propagation depth = 3 hops, decay = 0.75
		// Direct: 100 points on banTarget
		// Hop 1: 100 * 0.75 = 75.0 on voucher1
		// Hop 2: 100 * 0.75^2 = 56.25 on voucher2
		// Hop 3: 100 * 0.75^3 = 42.1875 on voucher3
		// voucher4 should NOT be affected (4 hops, beyond max depth 3)
		result, err := svcs.ModerationActionService.TakeAction(
			ctx,
			council.ID,     // moderator (use council from outer scope)
			banTarget.ID,   // target
			domain.ActionBan,
			5,              // severity
			"test ban for propagation",
			nil,            // bans have no duration
		)
		if err != nil {
			t.Fatalf("taking ban action: %v", err)
		}

		// We expect 4 penalties: direct + 3 hops.
		if len(result.Penalties) != 4 {
			t.Fatalf("expected 4 penalties, got %d", len(result.Penalties))
		}

		// Build a map of penalties by user for easier checking.
		penaltyByUser := map[string]domain.TrustPenalty{}
		for _, p := range result.Penalties {
			penaltyByUser[p.UserID] = p
		}

		// Verify direct penalty.
		if p, ok := penaltyByUser[banTarget.ID]; !ok {
			t.Error("missing direct penalty on banTarget")
		} else {
			if p.PenaltyAmount != 100.0 {
				t.Errorf("banTarget penalty: expected 100.0, got %.2f", p.PenaltyAmount)
			}
			if p.DecaysAt != nil {
				t.Error("severity 5 penalty should not decay (permanent)")
			}
		}

		// Verify hop 1 penalty.
		if p, ok := penaltyByUser[voucher1.ID]; !ok {
			t.Error("missing penalty on voucher1 (hop 1)")
		} else {
			if p.HopDepth != 1 {
				t.Errorf("voucher1 hop depth: expected 1, got %d", p.HopDepth)
			}
			expected := 100.0 * 0.75
			if p.PenaltyAmount != expected {
				t.Errorf("voucher1 penalty: expected %.2f, got %.2f", expected, p.PenaltyAmount)
			}
		}

		// Verify hop 2 penalty.
		if p, ok := penaltyByUser[voucher2.ID]; !ok {
			t.Error("missing penalty on voucher2 (hop 2)")
		} else {
			if p.HopDepth != 2 {
				t.Errorf("voucher2 hop depth: expected 2, got %d", p.HopDepth)
			}
			expected := 100.0 * 0.75 * 0.75
			if p.PenaltyAmount != expected {
				t.Errorf("voucher2 penalty: expected %.2f, got %.2f", expected, p.PenaltyAmount)
			}
		}

		// Verify hop 3 penalty.
		if p, ok := penaltyByUser[voucher3.ID]; !ok {
			t.Error("missing penalty on voucher3 (hop 3)")
		} else {
			if p.HopDepth != 3 {
				t.Errorf("voucher3 hop depth: expected 3, got %d", p.HopDepth)
			}
			expected := 100.0 * 0.75 * 0.75 * 0.75
			if p.PenaltyAmount != expected {
				t.Errorf("voucher3 penalty: expected %.4f, got %.4f", expected, p.PenaltyAmount)
			}
		}

		// Verify voucher4 was NOT penalized.
		if _, ok := penaltyByUser[voucher4.ID]; ok {
			t.Error("voucher4 should not have received a penalty (beyond 3-hop propagation depth)")
		}
	})
}

// TestGraphCycleDetection verifies that the AGE graph detects cycles and
// prevents circular vouching.
func TestGraphCycleDetection(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	svcs := newTestServices(pool)

	userA := testUser(t, pool, uniqueKratosID("cycleA"), domain.RoleMember, 80.0)
	userB := testUser(t, pool, uniqueKratosID("cycleB"), domain.RoleMember, 80.0)

	// A vouches for B.
	if _, err := svcs.VouchService.Vouch(ctx, userA.ID, userB.ID); err != nil {
		t.Fatalf("A vouching for B: %v", err)
	}

	// B trying to vouch for A should fail (creates a cycle).
	_, err := svcs.VouchService.Vouch(ctx, userB.ID, userA.ID)
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

// TestVouchEdgeCreationAndRemoval verifies that vouch edges are correctly
// created and removed from the AGE graph.
func TestVouchEdgeCreationAndRemoval(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	svcs := newTestServices(pool)

	voucher := testUser(t, pool, uniqueKratosID("edgeVoucher"), domain.RoleMember, 80.0)
	vouchee := testUser(t, pool, uniqueKratosID("edgeVouchee"), domain.RolePending, 50.0)

	// Create a vouch.
	vouch, err := svcs.VouchService.Vouch(ctx, voucher.ID, vouchee.ID)
	if err != nil {
		t.Fatalf("creating vouch: %v", err)
	}

	// Verify edge exists by checking FindVouchersWithDepth.
	vouchers, err := svcs.AGEQuerier.FindVouchersWithDepth(ctx, vouchee.ID, 1)
	if err != nil {
		t.Fatalf("finding vouchers: %v", err)
	}

	if _, ok := vouchers[voucher.ID]; !ok {
		t.Error("expected voucher to be found in graph after vouch creation")
	}

	// Revoke the vouch.
	if err := svcs.VouchService.Revoke(ctx, vouch.ID, voucher.ID); err != nil {
		t.Fatalf("revoking vouch: %v", err)
	}

	// Verify edge was removed.
	vouchers, err = svcs.AGEQuerier.FindVouchersWithDepth(ctx, vouchee.ID, 1)
	if err != nil {
		t.Fatalf("finding vouchers after revocation: %v", err)
	}

	if _, ok := vouchers[voucher.ID]; ok {
		t.Error("voucher should not be found in graph after vouch revocation")
	}
}

// TestTransitiveVouchTraversal verifies that AGE correctly traverses multi-hop
// vouch chains.
func TestTransitiveVouchTraversal(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	svcs := newTestServices(pool)

	// Chain: A -> B -> C -> D
	userA := testUser(t, pool, uniqueKratosID("transA"), domain.RoleMember, 80.0)
	userB := testUser(t, pool, uniqueKratosID("transB"), domain.RoleMember, 80.0)
	userC := testUser(t, pool, uniqueKratosID("transC"), domain.RoleMember, 80.0)
	userD := testUser(t, pool, uniqueKratosID("transD"), domain.RolePending, 50.0)

	if _, err := svcs.VouchService.Vouch(ctx, userC.ID, userD.ID); err != nil {
		t.Fatalf("C vouching for D: %v", err)
	}
	if _, err := svcs.VouchService.Vouch(ctx, userB.ID, userC.ID); err != nil {
		t.Fatalf("B vouching for C: %v", err)
	}
	if _, err := svcs.VouchService.Vouch(ctx, userA.ID, userB.ID); err != nil {
		t.Fatalf("A vouching for B: %v", err)
	}

	t.Run("depth 1 finds only direct voucher", func(t *testing.T) {
		result, err := svcs.AGEQuerier.FindVouchersWithDepth(ctx, userD.ID, 1)
		if err != nil {
			t.Fatalf("querying depth 1: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("expected 1 voucher at depth 1, got %d", len(result))
		}
		if _, ok := result[userC.ID]; !ok {
			t.Error("expected userC as direct voucher of userD")
		}
	})

	t.Run("depth 2 finds 2 vouchers", func(t *testing.T) {
		result, err := svcs.AGEQuerier.FindVouchersWithDepth(ctx, userD.ID, 2)
		if err != nil {
			t.Fatalf("querying depth 2: %v", err)
		}

		if len(result) != 2 {
			t.Fatalf("expected 2 vouchers at depth 2, got %d", len(result))
		}
		if result[userC.ID] != 1 {
			t.Errorf("userC depth: expected 1, got %d", result[userC.ID])
		}
		if result[userB.ID] != 2 {
			t.Errorf("userB depth: expected 2, got %d", result[userB.ID])
		}
	})

	t.Run("depth 3 finds all 3 vouchers", func(t *testing.T) {
		result, err := svcs.AGEQuerier.FindVouchersWithDepth(ctx, userD.ID, 3)
		if err != nil {
			t.Fatalf("querying depth 3: %v", err)
		}

		if len(result) != 3 {
			t.Fatalf("expected 3 vouchers at depth 3, got %d", len(result))
		}
		if result[userC.ID] != 1 {
			t.Errorf("userC depth: expected 1, got %d", result[userC.ID])
		}
		if result[userB.ID] != 2 {
			t.Errorf("userB depth: expected 2, got %d", result[userB.ID])
		}
		if result[userA.ID] != 3 {
			t.Errorf("userA depth: expected 3, got %d", result[userA.ID])
		}
	})
}
