package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/service"
)

// Build's whole point is that one graph serves the CLI and the test harness, so
// its preconditions must fail loudly rather than produce a half-wired Deps that
// nil-panics later, deep inside a request.
func TestBuild_RequiresItsCollaborators(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	tests := []struct {
		name   string
		logger *slog.Logger
	}{
		{"nil pool", logger},
		{"nil pool and nil logger", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := Build(config.Config{}, nil, nil, tt.logger)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if deps != nil {
				t.Errorf("deps = %+v, want nil alongside the error", deps)
			}
		})
	}
}

// trustInputs exists because service.TrustInputs spans five repositories and no
// single one satisfies it. If a repository stops providing its share — or two
// of them start colliding on a method name, making the promoted selector
// ambiguous — this stops compiling, which is the point.
func TestTrustInputsSatisfiesTheInterface(t *testing.T) {
	var _ service.TrustInputs = trustInputs{}
}
