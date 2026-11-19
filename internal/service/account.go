package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

var (
	errWrongPassword = apperr.New(apperr.Forbidden, "password is incorrect")
	errNoPassword    = invalid("account has no password")
)

type AccountDeps struct {
	Users    UserRepository
	Sessions RefreshTokenRepository
	Hasher   PasswordHasher
	Auth     *AuthService // issues the new session after a password change
	Now      func() time.Time
	Log      *zap.Logger
}

type ChangePasswordInput struct {
	CurrentPassword, NewPassword string
	DeviceID                     uuid.UUID
	RememberMe                   bool
}

// Account covers the signed-in user's own account: changing the password and deleting it.
type Account struct{ AccountDeps }

func NewAccount(d AccountDeps) *Account { return &Account{d} }

// ChangePassword checks the current password, sets the new one and signs the user out everywhere,
// then returns a fresh session for the device that asked. A wrong current password is 403, not 401,
// so a client doesn't mistake it for an expired access token and try to refresh.
func (a *Account) ChangePassword(ctx context.Context, userID uuid.UUID, in ChangePasswordInput) (Session, error) {
	if in.DeviceID == uuid.Nil {
		return Session{}, invalid("deviceId is required")
	}
	if err := validatePassword(in.NewPassword); err != nil {
		return Session{}, err
	}
	u, err := a.owner(ctx, userID)
	if err != nil {
		return Session{}, err
	}
	if u.PasswordHash == nil {
		return Session{}, errNoPassword
	}
	if err := a.checkPassword(in.CurrentPassword, *u.PasswordHash); err != nil {
		return Session{}, err
	}
	hash, err := a.Hasher.Hash(in.NewPassword)
	if err != nil {
		return Session{}, err
	}
	if err := a.Users.SetPassword(ctx, u.ID, hash, a.Now()); err != nil {
		return Session{}, fmt.Errorf("set password: %w", err)
	}
	if err := a.Sessions.RevokeAllForUser(ctx, u.ID, a.Now()); err != nil {
		return Session{}, fmt.Errorf("revoke sessions: %w", err)
	}
	a.Log.Info("password changed, all sessions revoked", zap.String("user_id", u.ID.String()))
	return a.Auth.startSession(ctx, u, in.DeviceID, lifetime(in.RememberMe))
}

// Delete permanently removes the account. Accounts with a password must confirm it; accounts without one
// (Apple/Google sign-in) need only their access token.
func (a *Account) Delete(ctx context.Context, userID uuid.UUID, password string) error {
	u, err := a.owner(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash != nil {
		if err := a.checkPassword(password, *u.PasswordHash); err != nil {
			return err
		}
	}
	err = a.Users.Delete(ctx, u.ID)
	if err != nil && !errors.Is(err, ErrNotFound) { // already gone is fine
		return fmt.Errorf("delete user: %w", err)
	}
	a.Log.Info("account deleted", zap.String("user_id", u.ID.String()))
	return nil
}

// owner loads the caller's own user; a valid token for a user that no longer exists is a 401.
func (a *Account) owner(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := a.Users.GetByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return User{}, errInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return u, nil
}

func (a *Account) checkPassword(password, hash string) error {
	ok, err := a.Hasher.Verify(password, hash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return errWrongPassword
	}
	return nil
}
