package social

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/StreamNext/Backend/internal/service"
)

const (
	appleIssuer   = "https://appleid.apple.com"
	appleKeysURL  = "https://appleid.apple.com/auth/keys"
	googleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"
	clockLeeway   = 30 * time.Second
	signingMethod = "RS256" // both providers sign with RS256; nothing else is accepted
)

// Google documents both spellings of its issuer.
var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// Config says which providers are enabled. A provider whose client id is empty is disabled.
type Config struct {
	AppleClientID   string
	GoogleClientIDs []string
	// Test overrides; empty means the providers' real key endpoints.
	AppleKeysURL, GoogleKeysURL string
}

// Verifier implements service.IdentityVerifier.
type Verifier struct {
	providers map[service.Provider]*provider
	now       func() time.Time
}

type provider struct {
	issuers, audiences []string
	keys               *keySet
	bindsNonce         bool // Apple: the token carries SHA-256(nonce) and the client must send the raw nonce
}

func New(cfg Config, client *http.Client, now func() time.Time) *Verifier {
	v := &Verifier{providers: map[service.Provider]*provider{}, now: now}
	if cfg.AppleClientID != "" {
		v.providers[service.ProviderApple] = &provider{
			issuers: []string{appleIssuer}, audiences: []string{cfg.AppleClientID}, bindsNonce: true,
			keys: newKeySet(orDefault(cfg.AppleKeysURL, appleKeysURL), client, now),
		}
	}
	if len(cfg.GoogleClientIDs) > 0 {
		v.providers[service.ProviderGoogle] = &provider{
			issuers: googleIssuers, audiences: cfg.GoogleClientIDs,
			keys: newKeySet(orDefault(cfg.GoogleKeysURL, googleKeysURL), client, now),
		}
	}
	return v
}

type tokenClaims struct {
	jwt.RegisteredClaims
	Email         string   `json:"email"`
	EmailVerified flexBool `json:"email_verified"`
	Nonce         string   `json:"nonce"`
	Name          string   `json:"name"`
}

// flexBool reads a JSON boolean that may also arrive as the string "true" (Apple sometimes does this).
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	*b = strings.Trim(string(data), `"`) == "true"
	return nil
}

func (v *Verifier) Verify(ctx context.Context, p service.Provider, token, nonce string) (service.Identity, error) {
	pr, ok := v.providers[p]
	if !ok {
		return service.Identity{}, service.ErrProviderDisabled
	}
	if pr.bindsNonce && nonce == "" {
		return service.Identity{}, invalidToken("missing nonce")
	}

	var keyErr error
	var claims tokenClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{signingMethod}),
		jwt.WithTimeFunc(v.now),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockLeeway),
	)
	_, err := parser.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token has no key id")
		}
		key, err := pr.keys.key(ctx, kid)
		keyErr = err
		return key, err
	})
	if errors.Is(keyErr, service.ErrVerifierUnavailable) {
		return service.Identity{}, keyErr
	}
	if err != nil {
		return service.Identity{}, invalidToken("%v", err)
	}

	if !slices.Contains(pr.issuers, claims.Issuer) {
		return service.Identity{}, invalidToken("wrong issuer")
	}
	if !slices.ContainsFunc(claims.Audience, func(a string) bool { return slices.Contains(pr.audiences, a) }) {
		return service.Identity{}, invalidToken("wrong audience")
	}
	if claims.Subject == "" {
		return service.Identity{}, invalidToken("no subject")
	}
	if pr.bindsNonce && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(hashNonce(nonce))) != 1 {
		return service.Identity{}, invalidToken("nonce mismatch")
	}
	return service.Identity{
		Provider: p, Subject: claims.Subject, Email: claims.Email, EmailVerified: bool(claims.EmailVerified), Name: claims.Name,
	}, nil
}

func hashNonce(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func invalidToken(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{service.ErrInvalidIdentityToken}, args...)...)
}

func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
