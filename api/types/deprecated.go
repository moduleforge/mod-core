package types

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// DeprecatedTypeSQLState is the SQLSTATE the entities_check_concrete_type
// trigger (model/migrations/0001_baseline.sql) raises when an entity is
// created with a deprecated fundamental type. It is a custom code in a
// non-standard class.
const DeprecatedTypeSQLState = "MF001"

// IsDeprecatedTypeError reports whether err is (or wraps) the Postgres error
// raised when creating an entity of a deprecated type. Callers map it to an
// API error of their choosing.
func IsDeprecatedTypeError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == DeprecatedTypeSQLState
}
