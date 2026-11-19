package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type PasswordResetDeps struct {
	Users    UserRepository
	Sessions RefreshTokenRepository
	Tokens   EmailTokenRepository
	Mailer   Mailer
	Hasher   PasswordHasher
	BaseURL  string
	Now      func() time.Time
	Log      *zap.Logger
}

// PasswordReset handles "forgot password" and the reset itself.
type PasswordReset struct {
	PasswordResetDeps
	tokens emailTokens
}

func NewPasswordReset(d PasswordResetDeps) *PasswordReset {
	return &PasswordReset{PasswordResetDeps: d, tokens: emailTokens{repo: d.Tokens, now: d.Now}}
}

// Request emails a reset link. Like Resend, it answers the same way whether or not the address has an
// account. Accounts without a password (Apple/Google sign-in) get one too: the email proves ownership, and
// resetting is how such an account sets a first password.
func (p *PasswordReset) Request(ctx context.Context, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	u, err := p.Users.GetByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	raw, err := p.tokens.issue(ctx, u.ID, PurposeReset, resetTokenTTL)
	if err != nil || raw == "" {
		return err
	}
	if err := p.Mailer.SendPasswordReset(ctx, u.Email, u.DisplayName, emailLink(p.BaseURL, "/reset-password", raw)); err != nil {
		p.Log.Error("could not send password reset email", zap.String("user_id", u.ID.String()), zap.Error(err))
	}
	return nil
}

// Reset sets a new password with the emailed token, then signs the user out everywhere. A weak password is
// rejected before the token is used, so the user can try again with the same link.
func (p *PasswordReset) Reset(ctx context.Context, rawToken, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	id, err := p.tokens.consume(ctx, rawToken, PurposeReset)
	if err != nil {
		return err
	}
	hash, err := p.Hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	err = p.Users.SetPassword(ctx, id, hash, p.Now())
	if errors.Is(err, ErrNotFound) { // the account was deleted after the email went out
		return errInvalidEmailToken
	}
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if err := p.Sessions.RevokeAllForUser(ctx, id, p.Now()); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	p.Log.Info("password reset, all sessions revoked", zap.String("user_id", id.String()))
	return nil
}
