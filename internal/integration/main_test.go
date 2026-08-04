//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/fireynis/the-bell/internal/testsupport"
)

// TestMain delegates to testsupport so the whole package shares one Postgres
// and one Redis container instead of starting a pair per test.
func TestMain(m *testing.M) {
	os.Exit(testsupport.RunMain(m))
}
