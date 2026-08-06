package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestValidateRemovalReason(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		want    string
		wantErr bool
	}{
		{"ordinary reason passes through", "harassment", "harassment", false},
		{"surrounding whitespace is trimmed off", "  harassment\n", "harassment", false},
		{"interior whitespace is left alone", "third\tstrike", "third\tstrike", false},
		{"empty reason is rejected", "", "", true},
		{"whitespace-only reason is rejected", " \t\n ", "", true},
		{"exactly at the length limit is accepted", strings.Repeat("a", maxRemovalReasonLen), strings.Repeat("a", maxRemovalReasonLen), false},
		{"one byte over the limit is rejected", strings.Repeat("a", maxRemovalReasonLen+1), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateRemovalReason(tt.reason)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("validateRemovalReason() error = %v, want %v", err, ErrValidation)
				}
				if got != "" {
					t.Errorf("validateRemovalReason() = %q, want empty on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateRemovalReason() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("validateRemovalReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A removal reason is mandatory, unlike an author's own deletion which stores
// "". The reason is the only record of why a moderator overrode a member's
// speech, so an empty one makes the removal unaccountable.
func TestValidateRemovalReason_IsMandatoryUnlikeAnAuthorDeletion(t *testing.T) {
	if _, err := validateRemovalReason(""); !errors.Is(err, ErrValidation) {
		t.Errorf("validateRemovalReason(\"\") error = %v, want %v", err, ErrValidation)
	}
}

func TestCanRemoveByModerator(t *testing.T) {
	tests := []struct {
		name   string
		status domain.PostStatus
		want   bool
	}{
		{"a visible post can be removed", domain.PostVisible, true},
		{"a post the author already deleted cannot be removed again", domain.PostRemovedByAuthor, false},
		{"a post another moderator already removed cannot be removed again", domain.PostRemovedByMod, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canRemoveByModerator(tt.status); got != tt.want {
				t.Errorf("canRemoveByModerator(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
