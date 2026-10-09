package types_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moduleforge/core-api/types"
)

func TestIsDeprecatedTypeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"direct", &pgconn.PgError{Code: types.DeprecatedTypeSQLState}, true},
		{"wrapped", fmt.Errorf("create: %w", &pgconn.PgError{Code: "MF001"}), true},
		{"other code", &pgconn.PgError{Code: "23505"}, false},
		{"non-pg error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := types.IsDeprecatedTypeError(tc.err); got != tc.want {
				t.Errorf("IsDeprecatedTypeError = %v, want %v", got, tc.want)
			}
		})
	}
}
