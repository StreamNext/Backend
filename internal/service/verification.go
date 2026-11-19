package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type VerificationDeps struct {
	Users   UserRepository
	Tokens  EmailTokenRepository
	Mailer  Mailer
	BaseURL string // where the email link points, e.g. https://streamnext.app
	Now     func() time.Time
	Log     *zap.Logger
}

// Verification sends and confirms the "verify your email" emails.
type Verification struct {
	VerificationDeps
	tokens emailTokens
}

func NewVerification(d VerificationDeps) *Verification {
	return &Verification{VerificationDeps: d, tokens: emailTokens{repo: d.Tokens, now: d.Now}}
}

// Send emails the user a verification link. It sends nothing if one went out within the cooldown.
func (v *Verification) Send(ctx context.Context, u User) error {
	raw, err := v.tokens.issue(ctx, u.ID, PurposeVerify, verifyTokenTTL)
	if err != nil || raw == "" {
		return err
	}
	return v.Mailer.SendVerification(ctx, u.Email, u.DisplayName, emailLink(v.BaseURL, "/verify-email", raw))
}

// Resend is the public "send me another verification email". It answers the same way whether or not the
// address has an account, so it can't be used to discover registered emails.
// shortcut: the email is sent inline, so response time can still hint that an account exists; move sending
// to a queue or background worker before a public launch.
func (v *Verification) Resend(ctx context.Context, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	u, err := v.Users.GetByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) || (err == nil && u.EmailVerifiedAt != nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if err := v.Send(ctx, u); err != nil {
		v.Log.Error("could not send verification email", zap.String("user_id", u.ID.String()), zap.Error(err))
	}
	return nil
}

// Confirm marks the owner's email verified. The token is single-use.
func (v *Verification) Confirm(ctx context.Context, rawToken string) error {
	id, err := v.tokens.consume(ctx, rawToken, PurposeVerify)
	if err != nil {
		return err
	}
	err = v.Users.MarkEmailVerified(ctx, id, v.Now())
	if errors.Is(err, ErrNotFound) { // the account was deleted after the email went out
		return errInvalidEmailToken
	}
	if err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	v.Log.Info("email verified", zap.String("user_id", id.String()))
	return nil
}
