package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

type EmailTokens struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewEmailTokens(pool *pgxpool.Pool) *EmailTokens {
	return &EmailTokens{pool: pool, q: sqlc.New(pool)}
}

// Replace supersedes the user's unused tokens of this purpose and stores the new one in one transaction.
func (r *EmailTokens) Replace(ctx context.Context, t service.NewEmailToken, now time.Time) error {
	return inTx(ctx, r.pool, r.q, func(q *sqlc.Queries) error {
		purpose := string(t.Purpose)
		if err := q.InvalidateUnusedEmailTokens(ctx, sqlc.InvalidateUnusedEmailTokensParams{UserID: t.UserID, Purpose: purpose, Now: now}); err != nil {
			return err
		}
		return q.InsertEmailToken(ctx, sqlc.InsertEmailTokenParams{
			UserID: t.UserID, Purpose: purpose, TokenHash: t.TokenHash, ExpiresAt: t.ExpiresAt, CreatedAt: now,
		})
	})
}

func (r *EmailTokens) LastCreated(ctx context.Context, userID uuid.UUID, purpose service.EmailPurpose) (time.Time, error) {
	at, err := r.q.LastEmailTokenCreatedAt(ctx, sqlc.LastEmailTokenCreatedAtParams{UserID: userID, Purpose: string(purpose)})
	if err != nil {
		return time.Time{}, notFound(err)
	}
	return at, nil
}

// Consume is one conditional UPDATE, so of two concurrent uses of the same token exactly one succeeds.
func (r *EmailTokens) Consume(ctx context.Context, tokenHash string, purpose service.EmailPurpose, now time.Time) (uuid.UUID, error) {
	id, err := r.q.ConsumeEmailToken(ctx, sqlc.ConsumeEmailTokenParams{TokenHash: tokenHash, Purpose: string(purpose), Now: now})
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	return id, nil
}
