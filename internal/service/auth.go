package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/auth"
)

const (
	shortRefreshLifetime = 24 * time.Hour
	longRefreshLifetime  = 90 * 24 * time.Hour // rememberMe
)

var (
	errInvalidCredentials = apperr.New(apperr.Unauthenticated, "invalid credentials")
	errInvalidRefresh     = apperr.New(apperr.Unauthenticated, "invalid refresh token")
)

type RegisterInput struct {
	Email, Password, DisplayName, Timezone string
	DeviceID                               uuid.UUID
	RememberMe                             bool
}

type LoginInput struct {
	Email, Password string
	DeviceID        uuid.UUID
	RememberMe      bool
}

type RefreshInput struct {
	RefreshToken string
	DeviceID     uuid.UUID
}

type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	User         User
}

// VerificationSender sends the "verify your email" message to a new account.
type VerificationSender interface {
	Send(ctx context.Context, u User) error
}

type AuthDeps struct {
	Users  UserRepository
	Tokens RefreshTokenRepository
	Hasher PasswordHasher
	Access AccessTokenIssuer
	// Grace is how long after a rotation the old token may be presented again (a lost response).
	Grace time.Duration
	Now   func() time.Time
	Log   *zap.Logger
	// Verifier emails new accounts a verification link. Optional: nil sends nothing.
	Verifier VerificationSender
}

// AuthService implements register, login, refresh and logout.
type AuthService struct{ AuthDeps }

func NewAuthService(d AuthDeps) *AuthService { return &AuthService{d} }

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (Session, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return Session{}, err
	}
	if err := validatePassword(in.Password); err != nil {
		return Session{}, err
	}
	if in.DeviceID == uuid.Nil {
		return Session{}, invalid("deviceId is required")
	}
	name := defaultDisplayName(email)
	if in.DisplayName != "" {
		if name, err = normalizeDisplayName(in.DisplayName); err != nil {
			return Session{}, err
		}
	}
	tz, err := normalizeTimezone(in.Timezone)
	if err != nil {
		return Session{}, err
	}

	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return Session{}, err
	}
	user, err := s.Users.Create(ctx, NewUser{Email: email, PasswordHash: &hash, DisplayName: name, Timezone: tz})
	if errors.Is(err, ErrEmailTaken) {
		return Session{}, apperr.New(apperr.Conflict, "email already registered")
	}
	if err != nil {
		return Session{}, fmt.Errorf("create user: %w", err)
	}
	s.Log.Info("user registered", zap.String("user_id", user.ID.String()))
	if s.Verifier != nil { // a mail failure must never block sign-up: the user can ask for another email
		if err := s.Verifier.Send(ctx, user); err != nil {
			s.Log.Error("could not send verification email", zap.String("user_id", user.ID.String()), zap.Error(err))
		}
	}
	return s.startSession(ctx, user, in.DeviceID, lifetime(in.RememberMe))
}

func (s *AuthService) Login(ctx context.Context, in LoginInput) (Session, error) {
	if in.DeviceID == uuid.Nil {
		return Session{}, invalid("deviceId is required")
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		s.Hasher.Dummy(in.Password) // keep timing the same as a real attempt
		return Session{}, errInvalidCredentials
	}
	user, err := s.Users.GetByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) || (err == nil && user.PasswordHash == nil) {
		s.Hasher.Dummy(in.Password) // unknown email or social-only account: same work, same error
		return Session{}, errInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("find user: %w", err)
	}
	ok, err := s.Hasher.Verify(in.Password, *user.PasswordHash)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return Session{}, errInvalidCredentials
	}
	return s.startSession(ctx, user, in.DeviceID, lifetime(in.RememberMe))
}

// Refresh rotates the presented token. See docs/features/auth.md for the reuse and grace rules.
func (s *AuthService) Refresh(ctx context.Context, in RefreshInput) (Session, error) {
	t, err := s.Tokens.GetByHash(ctx, auth.HashToken(in.RefreshToken))
	if errors.Is(err, ErrNotFound) {
		return Session{}, errInvalidRefresh
	}
	if err != nil {
		return Session{}, fmt.Errorf("find refresh token: %w", err)
	}
	if t.DeviceID != in.DeviceID { // same answer as an unknown token, and no side effects
		return Session{}, errInvalidRefresh
	}

	if t.RevokedAt == nil {
		if !s.Now().Before(t.ExpiresAt) {
			return Session{}, errInvalidRefresh
		}
		sess, ok, err := s.rotate(ctx, t)
		if err != nil || ok {
			return sess, err
		}
		// Lost a race with another refresh of the same token: it is revoked now.
		if t, err = s.Tokens.GetByID(ctx, t.ID); err != nil {
			return Session{}, fmt.Errorf("reload refresh token: %w", err)
		}
	}
	return s.refreshRevoked(ctx, t)
}

// refreshRevoked handles a token that was already revoked: a client retrying after a lost response (grace
// window), reuse of a rotated token (revoke everything), or simply a stale token (reject it).
func (s *AuthService) refreshRevoked(ctx context.Context, t RefreshToken) (Session, error) {
	now := s.Now()
	if t.ReplacedBy != nil && t.RevokedAt != nil && now.Sub(*t.RevokedAt) < s.Grace {
		succ, err := s.Tokens.GetByID(ctx, *t.ReplacedBy)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Session{}, fmt.Errorf("find successor token: %w", err)
		}
		if err == nil && succ.RevokedAt == nil && now.Before(succ.ExpiresAt) {
			// The successor was never used, so the client never got it: replace it.
			sess, ok, err := s.rotate(ctx, succ)
			if err != nil || ok {
				return sess, err
			}
		}
	}
	if t.ReplacedBy == nil {
		// Revoked by logout, a new login on the device, or a password change/reset, not by rotation. Nobody
		// holds a second copy of it, so this is a stale client rather than theft: reject it without signing
		// the user out of the sessions they created since.
		return Session{}, errInvalidRefresh
	}
	s.Log.Warn("refresh token reuse detected, revoking all sessions", zap.String("user_id", t.UserID.String()))
	if err := s.Tokens.RevokeAllForUser(ctx, t.UserID, now); err != nil {
		return Session{}, fmt.Errorf("revoke all tokens: %w", err)
	}
	return Session{}, errInvalidRefresh
}

// Logout revokes one refresh token. It is idempotent: an unknown or already revoked token is fine.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	t, err := s.Tokens.GetByHash(ctx, auth.HashToken(refreshToken))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find refresh token: %w", err)
	}
	return s.Tokens.Revoke(ctx, t.ID, s.Now())
}

// rotate replaces the live token old with a new one carrying the same lifetime, so the window slides.
// ok is false when old stopped being live in the meantime.
func (s *AuthService) rotate(ctx context.Context, old RefreshToken) (sess Session, ok bool, err error) {
	user, err := s.Users.GetByID(ctx, old.UserID)
	if errors.Is(err, ErrNotFound) {
		return Session{}, false, errInvalidRefresh
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("find user: %w", err)
	}
	raw, grant, err := s.newGrant(user.ID, old.DeviceID, time.Duration(old.LifetimeSeconds)*time.Second)
	if err != nil {
		return Session{}, false, err
	}
	ok, err = s.Tokens.Rotate(ctx, old.ID, grant, s.Now())
	if err != nil || !ok {
		return Session{}, false, err
	}
	sess, err = s.sessionFor(user, raw)
	return sess, err == nil, err
}

// startSession issues the device's first (or replacement) refresh token plus an access token.
func (s *AuthService) startSession(ctx context.Context, user User, deviceID uuid.UUID, life time.Duration) (Session, error) {
	raw, grant, err := s.newGrant(user.ID, deviceID, life)
	if err != nil {
		return Session{}, err
	}
	if err := s.Tokens.Issue(ctx, grant, s.Now()); err != nil {
		return Session{}, fmt.Errorf("issue refresh token: %w", err)
	}
	return s.sessionFor(user, raw)
}

func (s *AuthService) newGrant(userID, deviceID uuid.UUID, life time.Duration) (string, NewToken, error) {
	raw, hash, err := auth.NewToken()
	if err != nil {
		return "", NewToken{}, err
	}
	return raw, NewToken{
		UserID: userID, DeviceID: deviceID, TokenHash: hash,
		LifetimeSeconds: int(life.Seconds()), ExpiresAt: s.Now().Add(life),
	}, nil
}

func (s *AuthService) sessionFor(user User, rawRefresh string) (Session, error) {
	access, expiresIn, err := s.Access.Issue(user.ID)
	if err != nil {
		return Session{}, err
	}
	return Session{AccessToken: access, RefreshToken: rawRefresh, ExpiresIn: expiresIn, User: user}, nil
}

func lifetime(rememberMe bool) time.Duration {
	if rememberMe {
		return longRefreshLifetime
	}
	return shortRefreshLifetime
}
