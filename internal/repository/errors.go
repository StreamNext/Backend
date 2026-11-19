// Package repository implements the service layer's repository interfaces over Postgres (sqlc + pgx).
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/StreamNext/Backend/internal/service"
)

// notFound turns "no rows" into the service-level sentinel; other errors pass through.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrNotFound
	}
	return err
}

// affected turns "no row matched" into ErrNotFound, for UPDATE and DELETE statements that return a row count.
func affected(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows == 0 {
		return service.ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
