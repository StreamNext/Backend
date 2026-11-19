package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StreamNext/Backend/internal/sqlc"
)

// inTx runs fn in a transaction: committed if it returns nil, rolled back otherwise.
func inTx(ctx context.Context, pool *pgxpool.Pool, q *sqlc.Queries, fn func(*sqlc.Queries) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit
	if err := fn(q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
