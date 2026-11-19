package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenIssuer signs and parses short-lived HS256 access tokens. The only custom claim is the
// user id, carried in `sub`.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenIssuer(secret string, ttl time.Duration, now func() time.Time) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), ttl: ttl, now: now}
}

// Issue returns a signed token and its lifetime in seconds.
func (t *TokenIssuer) Issue(userID uuid.UUID) (string, int, error) {
	now := t.now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", 0, fmt.Errorf("auth: sign jwt: %w", err)
	}
	return signed, int(t.ttl.Seconds()), nil
}

// Parse validates signature (HS256 only), expiry and subject, and returns the user id.
func (t *TokenIssuer) Parse(token string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(t.now),
		jwt.WithExpirationRequired(),
	)
	var claims jwt.RegisteredClaims
	if _, err := parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return t.secret, nil }); err != nil {
		return uuid.Nil, fmt.Errorf("auth: invalid token: %w", err)
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, errors.New("auth: invalid token subject")
	}
	return id, nil
}
