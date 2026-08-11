package postgres

import (
	"math"
	"testing"
)

func TestInt32Bound(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int32
	}{
		{"zero", 0, 0},
		{"a page size", 100, 100},
		{"the largest value that survives", math.MaxInt32, math.MaxInt32},
		// Without the bound these wrap negative, and Postgres rejects a
		// negative LIMIT or OFFSET rather than returning a page.
		{"one past the largest", math.MaxInt32 + 1, math.MaxInt32},
		{"far past the largest", math.MaxInt64, math.MaxInt32},
		{"negative", -1, 0},
		{"most negative", math.MinInt64, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := int32Bound(tt.in); got != tt.want {
				t.Errorf("int32Bound(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
