package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unique violation", &pgconn.PgError{Code: "23505"}, true},
		{"wrapped unique violation", fmt.Errorf("creating vouch: %w", &pgconn.PgError{Code: "23505"}), true},
		{"foreign key violation", &pgconn.PgError{Code: "23503"}, false},
		{"not-null violation", &pgconn.PgError{Code: "23502"}, false},
		{"no rows", pgx.ErrNoRows, false},
		{"plain error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueViolation(tt.err); got != tt.want {
				t.Errorf("isUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
