package types

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// NaturalPersonOwnerSQLState is the SQLSTATE the
// entities_owner_self_natural_person trigger
// (the entities_owner_self_natural_person trigger in model/migrations/0001_baseline.sql) raises when a
// natural_person entity would be owned by anything other than itself. It is a
// custom code in the same non-standard class as DeprecatedTypeSQLState.
const NaturalPersonOwnerSQLState = "MF002"

// IsNaturalPersonOwnerError reports whether err is (or wraps) the Postgres
// error raised when a natural_person entity is created with, or updated to, a
// non-self owner.
func IsNaturalPersonOwnerError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == NaturalPersonOwnerSQLState
}
