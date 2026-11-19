// Package service holds the business rules. It depends only on the interfaces declared here,
// so it is unit-tested with in-memory fakes; internal/repository implements them over Postgres.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrEmailTaken = errors.New("email already registered")
)

type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    *string // nil for social-only accounts
	EmailVerifiedAt *time.Time
	DisplayName     string
	Timezone        string
}

type RefreshToken struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	DeviceID        uuid.UUID
	TokenHash       string
	LifetimeSeconds int
	ExpiresAt       time.Time
	RevokedAt       *time.Time
	ReplacedBy      *uuid.UUID
}

// NewToken is what the service asks a repository to store; the repository assigns the id.
type NewToken struct {
	UserID          uuid.UUID
	DeviceID        uuid.UUID
	TokenHash       string
	LifetimeSeconds int
	ExpiresAt       time.Time
}

type UserRepository interface {
	// Create returns ErrEmailTaken if the email (case-insensitive) is already registered.
	Create(ctx context.Context, email string, passwordHash *string, displayName, timezone string) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error) // ErrNotFound
	GetByID(ctx context.Context, id uuid.UUID) (User, error)    // ErrNotFound
	// UpdateProfile changes only the non-nil fields.
	UpdateProfile(ctx context.Context, id uuid.UUID, displayName, timezone *string, now time.Time) (User, error)
}

// RefreshTokenRepository's methods are each atomic, so a crash never leaves a device with no
// token or with two live ones.
type RefreshTokenRepository interface {
	// Issue stores t as the device's only live token, revoking any previous live one.
	Issue(ctx context.Context, t NewToken, now time.Time) error
	GetByHash(ctx context.Context, hash string) (RefreshToken, error) // ErrNotFound
	GetByID(ctx context.Context, id uuid.UUID) (RefreshToken, error)  // ErrNotFound
	// Rotate revokes old and stores next, recording old.ReplacedBy. It returns false (and changes
	// nothing) if old is no longer live, e.g. a concurrent refresh already rotated it.
	Rotate(ctx context.Context, oldID uuid.UUID, next NewToken, now time.Time) (bool, error)
	// Revoke is a no-op for a missing or already revoked token.
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, phc string) (bool, error)
	Dummy(password string)
}

type AccessTokenIssuer interface {
	// Issue returns a signed access token and its lifetime in seconds.
	Issue(userID uuid.UUID) (string, int, error)
}
