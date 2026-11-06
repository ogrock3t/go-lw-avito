package trip

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	uniqueViolationCode = "23505"

	activeDriverConstraint = "one_active_trip_per_driver"
)

func isDriverBusyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == activeDriverConstraint
}
