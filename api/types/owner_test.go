package types_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moduleforge/core-api/types"
)

func TestIsNaturalPersonOwnerError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", fmt.Errorf("boom"), false},
		{"other pg code", &pgconn.PgError{Code: types.DeprecatedTypeSQLState}, false},
		{"direct", &pgconn.PgError{Code: types.NaturalPersonOwnerSQLState}, true},
		{"wrapped", fmt.Errorf("ctx: %w", &pgconn.PgError{Code: types.NaturalPersonOwnerSQLState}), true},
	}
	for _, c := range cases {
		if got := types.IsNaturalPersonOwnerError(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
