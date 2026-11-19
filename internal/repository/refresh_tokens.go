package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

type RefreshTokens struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewRefreshTokens(pool *pgxpool.Pool) *RefreshTokens {
	return &RefreshTokens{pool: pool, q: sqlc.New(pool)}
}

// Issue revokes the device's live token and stores the new one in one transaction.
func (r *RefreshTokens) Issue(ctx context.Context, t service.NewToken, now time.Time) error {
	return inTx(ctx, r.pool, r.q, func(q *sqlc.Queries) error {
		if err := q.RevokeActiveDeviceTokens(ctx, sqlc.RevokeActiveDeviceTokensParams{UserID: t.UserID, DeviceID: t.DeviceID, RevokedAt: &now}); err != nil {
			return err
		}
		_, err := q.InsertRefreshToken(ctx, insertParams(t, now))
		return err
	})
}

func (r *RefreshTokens) GetByHash(ctx context.Context, hash string) (service.RefreshToken, error) {
	row, err := r.q.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return service.RefreshToken{}, notFound(err)
	}
	return toToken(row), nil
}

func (r *RefreshTokens) GetByID(ctx context.Context, id uuid.UUID) (service.RefreshToken, error) {
	row, err := r.q.GetRefreshTokenByID(ctx, id)
	if err != nil {
		return service.RefreshToken{}, notFound(err)
	}
	return toToken(row), nil
}

// Rotate revokes old and stores next in one transaction. The conditional UPDATE takes a row
// lock, so of two concurrent rotations of the same token exactly one sees it live; the other gets false.
// The old row must be revoked before the insert, or the one-live-token-per-device index would reject it.
func (r *RefreshTokens) Rotate(ctx context.Context, oldID uuid.UUID, next service.NewToken, now time.Time) (bool, error) {
	rotated := false
	err := inTx(ctx, r.pool, r.q, func(q *sqlc.Queries) error {
		n, err := q.RevokeTokenIfActive(ctx, sqlc.RevokeTokenIfActiveParams{ID: oldID, RevokedAt: &now})
		if err != nil || n == 0 {
			return err // n == 0: not live any more; the transaction rolls back with nothing changed
		}
		row, err := q.InsertRefreshToken(ctx, insertParams(next, now))
		if err != nil {
			return err
		}
		if err := q.SetTokenReplacedBy(ctx, sqlc.SetTokenReplacedByParams{ID: oldID, ReplacedBy: &row.ID}); err != nil {
			return err
		}
		rotated = true
		return nil
	})
	return rotated && err == nil, err
}

func (r *RefreshTokens) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := r.q.RevokeTokenIfActive(ctx, sqlc.RevokeTokenIfActiveParams{ID: id, RevokedAt: &now})
	return err
}

func (r *RefreshTokens) RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error {
	return r.q.RevokeAllUserTokens(ctx, sqlc.RevokeAllUserTokensParams{UserID: userID, RevokedAt: &now})
}

func insertParams(t service.NewToken, now time.Time) sqlc.InsertRefreshTokenParams {
	return sqlc.InsertRefreshTokenParams{
		UserID: t.UserID, DeviceID: t.DeviceID, TokenHash: t.TokenHash,
		LifetimeSeconds: int32(t.LifetimeSeconds), ExpiresAt: t.ExpiresAt, CreatedAt: now,
	}
}

func toToken(t sqlc.RefreshToken) service.RefreshToken {
	return service.RefreshToken{
		ID: t.ID, UserID: t.UserID, DeviceID: t.DeviceID, TokenHash: t.TokenHash,
		LifetimeSeconds: int(t.LifetimeSeconds), ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, ReplacedBy: t.ReplacedBy,
	}
}
