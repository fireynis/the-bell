package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func residencyService(t *testing.T) (*UserService, *fakeUserStore) {
	t.Helper()

	store := newFakeUserStore()
	store.add(&domain.User{ID: "resident-1", DisplayName: "Ada", Role: domain.RolePending, IsActive: true})
	return NewUserService(store, nil), store
}

func TestUserService_UpdateResidencyClaim(t *testing.T) {
	tests := []struct {
		name    string
		claim   string
		want    string
		wantErr bool
	}{
		{"a plain address", "12 Mill Lane", "12 Mill Lane", false},
		{"trimmed", "   12 Mill Lane\n ", "12 Mill Lane", false},
		{
			"a landmark rather than an address",
			"the blue house behind the old mill",
			"the blue house behind the old mill",
			false,
		},
		{"at the limit", strings.Repeat("a", maxResidencyClaimLength), strings.Repeat("a", maxResidencyClaimLength), false},
		{"one over the limit", strings.Repeat("a", maxResidencyClaimLength+1), "", true},
		// Counted in runes, so a claim written in a non-Latin script gets the
		// same allowance as one written in English. Counted in bytes this
		// would be nearly three times over.
		{"multi-byte at the limit", strings.Repeat("é", maxResidencyClaimLength), strings.Repeat("é", maxResidencyClaimLength), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := residencyService(t)

			err := svc.UpdateResidencyClaim(context.Background(), "resident-1", tt.claim)

			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("error = %v, want ErrValidation", err)
				}
				if got := store.users["resident-1"].ResidencyClaim; got != "" {
					t.Errorf("a rejected claim was stored anyway: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateResidencyClaim: %v", err)
			}
			if got := store.users["resident-1"].ResidencyClaim; got != tt.want {
				t.Errorf("stored claim = %q, want %q", got, tt.want)
			}
		})
	}
}

// Clearing is not an error. Withdrawing what you said about where you live has
// to be as easy as saying it, and a resident having second thoughts about a
// pending application should not need an administrator.
func TestUserService_UpdateResidencyClaim_EmptyClears(t *testing.T) {
	for _, claim := range []string{"", "   ", "\n\t "} {
		svc, store := residencyService(t)
		store.users["resident-1"].ResidencyClaim = "12 Mill Lane"

		if err := svc.UpdateResidencyClaim(context.Background(), "resident-1", claim); err != nil {
			t.Fatalf("UpdateResidencyClaim(%q): %v", claim, err)
		}
		if got := store.users["resident-1"].ResidencyClaim; got != "" {
			t.Errorf("claim = %q after clearing with %q, want empty", got, claim)
		}
	}
}

// Nothing tries to decide whether a claim is true. It is an attestation a human
// reviews, so a validator that demanded something address-shaped would refuse
// the most useful thing a neighbour can say — and refuse most of the world's
// address formats along with it.
func TestUserService_UpdateResidencyClaim_DoesNotJudgeTheContent(t *testing.T) {
	claims := []string{
		"Flat 2b, 14 Rue de la Paix",
		"the farm at the end of Tanner's Road, past the cattle grid",
		"上海市静安区南京西路 1266 号",
		"same place as my brother Tom",
	}

	for _, claim := range claims {
		svc, store := residencyService(t)
		if err := svc.UpdateResidencyClaim(context.Background(), "resident-1", claim); err != nil {
			t.Errorf("UpdateResidencyClaim(%q) was refused: %v", claim, err)
			continue
		}
		if got := store.users["resident-1"].ResidencyClaim; got != claim {
			t.Errorf("stored %q, want it kept verbatim as %q", got, claim)
		}
	}
}

// A write that fails must reach the caller. A silently dropped claim would
// leave the resident believing the council can see something it cannot.
func TestUserService_UpdateResidencyClaim_UnknownUserPropagates(t *testing.T) {
	svc, _ := residencyService(t)

	err := svc.UpdateResidencyClaim(context.Background(), "nobody", "12 Mill Lane")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// Updating the claim must not disturb the profile, and updating the profile
// must not disturb the claim. They are separate writes because they have
// separate readers: the profile is public, the claim is for the reviewing
// council.
func TestUserService_ResidencyClaimAndProfileDoNotOverwriteEachOther(t *testing.T) {
	svc, store := residencyService(t)
	store.users["resident-1"].Bio = "keeps bees"

	if err := svc.UpdateResidencyClaim(context.Background(), "resident-1", "12 Mill Lane"); err != nil {
		t.Fatalf("UpdateResidencyClaim: %v", err)
	}
	if got := store.users["resident-1"].Bio; got != "keeps bees" {
		t.Errorf("bio = %q after a claim update, want it untouched", got)
	}

	if _, err := svc.UpdateProfile(context.Background(), "resident-1", "Ada L", "keeps bees and hens", ""); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if got := store.users["resident-1"].ResidencyClaim; got != "12 Mill Lane" {
		t.Errorf("claim = %q after a profile update, want it untouched", got)
	}
}
