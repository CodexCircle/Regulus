package storage

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is the database refusing a row because
// a unique or primary key constraint would be broken, on either backend.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505"
	}
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
