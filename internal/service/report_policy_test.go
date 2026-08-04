package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateReportReason(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		want    string
		wantErr bool
	}{
		{"ordinary reason passes through", "this is spam", "this is spam", false},
		{"surrounding whitespace is trimmed off", "  this is spam\n", "this is spam", false},
		{"interior whitespace is left alone", "this  is\tspam", "this  is\tspam", false},
		{"empty reason is rejected", "", "", true},
		{"whitespace-only reason is rejected", " \t\n ", "", true},
		{"exactly at the length limit is accepted", strings.Repeat("a", maxReportReasonLen), strings.Repeat("a", maxReportReasonLen), false},
		{"one byte over the limit is rejected", strings.Repeat("a", maxReportReasonLen+1), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateReportReason(tt.reason)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("validateReportReason() error = %v, want %v", err, ErrValidation)
				}
				if got != "" {
					t.Errorf("validateReportReason() = %q, want empty on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateReportReason() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("validateReportReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Trimming happens before the length check, so padding a maximum-length reason
// with whitespace must not push it over the limit.
func TestValidateReportReason_TrimsBeforeMeasuring(t *testing.T) {
	padded := "   " + strings.Repeat("a", maxReportReasonLen) + "   "

	got, err := validateReportReason(padded)
	if err != nil {
		t.Fatalf("validateReportReason() unexpected error: %v", err)
	}
	if len(got) != maxReportReasonLen {
		t.Errorf("len(got) = %d, want %d", len(got), maxReportReasonLen)
	}
}

// The limit is measured in bytes, matching the database column, so a reason of
// multi-byte characters is cut off well before 1000 visible characters. This
// pins the current contract rather than endorsing it — the error text says
// "characters", which is misleading for non-ASCII reasons.
func TestValidateReportReason_LimitIsBytesNotRunes(t *testing.T) {
	// 501 two-byte runes is 1002 bytes but only 501 characters.
	reason := strings.Repeat("é", 501)
	if len([]rune(reason)) > maxReportReasonLen {
		t.Fatalf("test setup: %d runes is already over the limit", len([]rune(reason)))
	}

	if _, err := validateReportReason(reason); !errors.Is(err, ErrValidation) {
		t.Errorf("validateReportReason(%d bytes / %d runes) error = %v, want %v",
			len(reason), len([]rune(reason)), err, ErrValidation)
	}
}
