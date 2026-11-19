package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/auth"
)

const (
	verifyTokenTTL = 24 * time.Hour
	resetTokenTTL  = time.Hour
	emailCooldown  = 60 * time.Second // at most one email per address and purpose in this window
)

var errInvalidEmailToken = apperr.New(apperr.InvalidArgument, "invalid or expired token")

// emailTokens is the shared machinery behind the verification and reset emails.
type emailTokens struct {
	repo EmailTokenRepository
	now  func() time.Time
}

// issue creates a single-use token for the user. If one of that purpose was created within the cooldown
// it creates nothing and returns "", and the caller sends no email.
func (e emailTokens) issue(ctx context.Context, userID uuid.UUID, purpose EmailPurpose, ttl time.Duration) (string, error) {
	last, err := e.repo.LastCreated(ctx, userID, purpose)
	switch {
	case err == nil && e.now().Sub(last) < emailCooldown:
		return "", nil
	case err != nil && !errors.Is(err, ErrNotFound):
		return "", fmt.Errorf("check email cooldown: %w", err)
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	now := e.now()
	if err := e.repo.Replace(ctx, NewEmailToken{UserID: userID, Purpose: purpose, TokenHash: hash, ExpiresAt: now.Add(ttl)}, now); err != nil {
		return "", fmt.Errorf("store email token: %w", err)
	}
	return raw, nil
}

// consume uses up a token and returns its owner. Any reason it can't be used gives the same 400.
func (e emailTokens) consume(ctx context.Context, raw string, purpose EmailPurpose) (uuid.UUID, error) {
	id, err := e.repo.Consume(ctx, auth.HashToken(raw), purpose, e.now())
	if errors.Is(err, ErrNotFound) {
		return uuid.Nil, errInvalidEmailToken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("consume email token: %w", err)
	}
	return id, nil
}

// emailLink is what goes in the email: a page of the Web repo, with the token in the query string.
func emailLink(baseURL, page, token string) string {
	return baseURL + page + "?token=" + url.QueryEscape(token)
}
