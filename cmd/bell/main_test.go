package main

import (
	"slices"
	"testing"
)

func TestParseCouncilEmails(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single address",
			raw:  "mayor@springfield.gov",
			want: []string{"mayor@springfield.gov"},
		},
		{
			name: "several addresses",
			raw:  "mayor@springfield.gov,clerk@springfield.gov",
			want: []string{"mayor@springfield.gov", "clerk@springfield.gov"},
		},
		{
			name: "surrounding whitespace is trimmed",
			raw:  "  mayor@springfield.gov ,\tclerk@springfield.gov  ",
			want: []string{"mayor@springfield.gov", "clerk@springfield.gov"},
		},
		{
			name: "trailing comma does not create a blank member",
			raw:  "mayor@springfield.gov,",
			want: []string{"mayor@springfield.gov"},
		},
		{
			name: "repeated separators are ignored",
			raw:  "mayor@springfield.gov,,,clerk@springfield.gov",
			want: []string{"mayor@springfield.gov", "clerk@springfield.gov"},
		},
		{
			name: "empty input yields no members",
			raw:  "",
			want: nil,
		},
		{
			name: "separators only yield no members",
			raw:  " , , ",
			want: nil,
		},
		{
			name: "order is preserved",
			raw:  "c@x.gov,a@x.gov,b@x.gov",
			want: []string{"c@x.gov", "a@x.gov", "b@x.gov"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCouncilEmails(tt.raw)
			if !slices.Equal(got, tt.want) {
				t.Errorf("parseCouncilEmails(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// Bootstrap creates one Kratos identity per address, so a repeated email would
// fail partway through with some council members already created.
func TestParseCouncilEmails_DropsDuplicates(t *testing.T) {
	got := parseCouncilEmails("mayor@springfield.gov,clerk@springfield.gov,mayor@springfield.gov")

	want := []string{"mayor@springfield.gov", "clerk@springfield.gov"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseCouncilEmails_DuplicatesAreCaseInsensitive(t *testing.T) {
	got := parseCouncilEmails("Mayor@Springfield.gov,mayor@springfield.gov")

	if len(got) != 1 {
		t.Fatalf("got %v, want a single address", got)
	}
	// The first spelling seen is the one kept, so the operator sees what they typed.
	if got[0] != "Mayor@Springfield.gov" {
		t.Errorf("got %q, want the first spelling %q", got[0], "Mayor@Springfield.gov")
	}
}
