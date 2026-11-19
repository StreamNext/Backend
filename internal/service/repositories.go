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
	Language        Language // en or es
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

// NewUser is what the service asks a repository to create.
type NewUser struct {
	Email           string
	PasswordHash    *string    // nil for accounts created through a provider
	EmailVerifiedAt *time.Time // nil until the address is confirmed
	DisplayName     string
	Timezone        string
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
	Create(ctx context.Context, u NewUser) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error) // ErrNotFound
	GetByID(ctx context.Context, id uuid.UUID) (User, error)    // ErrNotFound
	// UpdateProfile changes only the non-nil fields.
	UpdateProfile(ctx context.Context, id uuid.UUID, displayName, timezone, language *string, now time.Time) (User, error)
	SetPassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error // ErrNotFound
	// ClearPassword leaves the account without a password, so it can only be entered via a provider
	// or after a password reset.
	ClearPassword(ctx context.Context, id uuid.UUID, now time.Time) error // ErrNotFound
	// MarkEmailVerified is idempotent: a second call keeps the first verification time.
	MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error // ErrNotFound
	// Delete removes the user; refresh and email tokens go with it (cascade).
	Delete(ctx context.Context, id uuid.UUID) error // ErrNotFound
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

// EmailPurpose says what a single-use email token is for; a token only works for its own purpose.
type EmailPurpose string

const (
	PurposeVerify EmailPurpose = "verify"
	PurposeReset  EmailPurpose = "reset"
)

type NewEmailToken struct {
	UserID    uuid.UUID
	Purpose   EmailPurpose
	TokenHash string
	ExpiresAt time.Time
}

type EmailTokenRepository interface {
	// Replace stores t and invalidates the user's earlier unused tokens of the same purpose, atomically.
	Replace(ctx context.Context, t NewEmailToken, now time.Time) error
	// LastCreated is when the user's most recent token of that purpose was created; ErrNotFound if none.
	LastCreated(ctx context.Context, userID uuid.UUID, purpose EmailPurpose) (time.Time, error)
	// Consume atomically marks a live (unused, unexpired) token of that purpose as used and returns its
	// owner. ErrNotFound if the token is unknown, already used, superseded, expired, or for another purpose.
	Consume(ctx context.Context, tokenHash string, purpose EmailPurpose, now time.Time) (uuid.UUID, error)
}

// Mailer sends the application's emails; internal/mail implements it.
type Mailer interface {
	SendVerification(ctx context.Context, to, name, link string) error
	SendPasswordReset(ctx context.Context, to, name, link string) error
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

// --- social sign-in and devices ------------------------------------------------------------

var (
	ErrIdentityTaken        = errors.New("provider identity already linked")
	ErrProviderDisabled     = errors.New("sign-in provider is not configured")
	ErrInvalidIdentityToken = errors.New("invalid identity token")
	ErrVerifierUnavailable  = errors.New("cannot verify identity tokens right now")
)

type Provider string

const (
	ProviderApple  Provider = "apple"
	ProviderGoogle Provider = "google"
)

// Identity is who a provider says the signed-in person is, taken from a verified token.
type Identity struct {
	Provider      Provider
	Subject       string // the provider's stable id for the person
	Email         string
	EmailVerified bool
	Name          string
}

// IdentityVerifier checks a provider's ID token and returns the identity in it. It returns
// ErrProviderDisabled, ErrInvalidIdentityToken or ErrVerifierUnavailable (possibly wrapped) when it cannot.
// nonce is the raw nonce the client used, for providers that bind one (Apple).
type IdentityVerifier interface {
	Verify(ctx context.Context, provider Provider, idToken, nonce string) (Identity, error)
}

type OAuthRepository interface {
	// FindUser returns the user an identity is linked to; ErrNotFound if it is not linked.
	FindUser(ctx context.Context, provider Provider, subject string) (uuid.UUID, error)
	// Link ties an identity to a user. ErrIdentityTaken if it is already linked (to anyone).
	Link(ctx context.Context, userID uuid.UUID, id Identity, now time.Time) error
}

type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

type Device struct {
	UserID, DeviceID uuid.UUID
	FCMToken         string
	Platform         Platform
}

type DeviceRepository interface {
	// Upsert stores the device's push token for its user and removes that token from any other owner.
	// ErrNotFound if the user no longer exists.
	Upsert(ctx context.Context, d Device, now time.Time) error
	// Delete is a no-op if the device is not registered.
	Delete(ctx context.Context, userID, deviceID uuid.UUID) error
}
