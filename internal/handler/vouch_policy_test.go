package handler

import (
	"strings"
	"testing"
)

func TestRequireIDField_AcceptsAndTrims(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"plain id", "user-1", "user-1"},
		{"surrounding spaces", "  user-1  ", "user-1"},
		{"uuid", "0195f1a4-0000-7000-8000-000000000001", "0195f1a4-0000-7000-8000-000000000001"},
		{"newline padding", "\nuser-1\t", "user-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requireIDField("vouchee_id", tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("id = %q, want %q", got, tt.want)
			}
		})
	}
}

// A missing ID must be caught before the service sees it. Passing "" down
// returns ErrNotFound, which surfaces as 404 "not found" — that tells the
// caller the person they named does not exist, when in fact they named nobody.
func TestRequireIDField_RejectsBlankBeforeItBecomesA404(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"spaces only", "   "},
		{"tab and newline only", "\t\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := requireIDField("vouchee_id", tt.raw); err == nil {
				t.Fatal("expected an error for a blank id, got nil")
			}
		})
	}
}

// The message has to name the field, because the two endpoints reject
// different ones and "id is required" would not say which.
func TestRequireIDField_ErrorNamesTheField(t *testing.T) {
	_, err := requireIDField("vouchee_id", "")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "vouchee_id") {
		t.Errorf("error = %q, want it to name the vouchee_id field", err.Error())
	}
}
