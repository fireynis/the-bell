//go:build integration

package database_test

import (
	"os"
	"testing"

	"github.com/fireynis/the-bell/internal/testsupport"
)

// Delegating to RunMain releases the shared containers as soon as this binary
// finishes rather than leaving them to the Testcontainers reaper.
func TestMain(m *testing.M) { os.Exit(testsupport.RunMain(m)) }
