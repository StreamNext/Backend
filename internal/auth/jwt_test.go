package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "0123456789abcdef0123456789abcdef"

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func issuerAt(now time.Time) *TokenIssuer {
	return NewTokenIssuer(secret, 15*time.Minute, func() time.Time { return now })
}

func TestIssueAndParse(t *testing.T) {
	id := uuid.New()
	tok, expiresIn, err := issuerAt(t0).Issue(id)
	require.NoError(t, err)
	assert.Equal(t, 900, expiresIn)

	got, err := issuerAt(t0.Add(14 * time.Minute)).Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestParseRejectsExpired(t *testing.T) {
	tok, _, _ := issuerAt(t0).Issue(uuid.New())
	_, err := issuerAt(t0.Add(15*time.Minute + time.Second)).Parse(tok)
	assert.Error(t, err)
}

func TestParseRejectsWrongSecretAndGarbage(t *testing.T) {
	tok, _, _ := issuerAt(t0).Issue(uuid.New())
	other := NewTokenIssuer("a-completely-different-secret-value", time.Minute, func() time.Time { return t0 })
	_, err := other.Parse(tok)
	assert.Error(t, err)

	for _, bad := range []string{"", "not.a.jwt", tok + "x", tok[:len(tok)-4]} {
		_, err := issuerAt(t0).Parse(bad)
		assert.Error(t, err, bad)
	}
}

func TestParseRejectsOtherAlgorithms(t *testing.T) {
	claims := jwt.RegisteredClaims{Subject: uuid.NewString(), ExpiresAt: jwt.NewNumericDate(t0.Add(time.Hour))}

	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	_, err := issuerAt(t0).Parse(none)
	assert.Error(t, err, "alg=none must be rejected")

	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(secret))
	_, err = issuerAt(t0).Parse(hs512)
	assert.Error(t, err, "only HS256 is accepted, even with the right secret")
}

func TestParseRejectsMissingExpiryAndBadSubject(t *testing.T) {
	noExp, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: uuid.NewString()}).SignedString([]byte(secret))
	_, err := issuerAt(t0).Parse(noExp)
	assert.Error(t, err, "a token that never expires is invalid")

	badSub, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "not-a-uuid", ExpiresAt: jwt.NewNumericDate(t0.Add(time.Hour))}).SignedString([]byte(secret))
	_, err = issuerAt(t0).Parse(badSub)
	assert.ErrorContains(t, err, "subject")
}
