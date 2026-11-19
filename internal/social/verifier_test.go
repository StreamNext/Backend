package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

var (
	epoch = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	keyOnce    sync.Once
	keyA, keyB *rsa.PrivateKey
)

// testKeys generates two RSA keys once: A is the provider's key, B belongs to an attacker or a rotation.
func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if keyA, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if keyB, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return keyA, keyB
}

func jwkFor(kid string, k *rsa.PrivateKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
	}
}

// fakeProvider serves a JWKS document, can be switched to fail, and counts requests.
type fakeProvider struct {
	srv  *httptest.Server
	hits atomic.Int32
	mu   sync.Mutex
	keys []map[string]string
	fail int // HTTP status to answer with instead of the document; 0 = healthy
	body string
}

func newFakeProvider(t *testing.T, keys ...map[string]string) *fakeProvider {
	t.Helper()
	p := &fakeProvider{keys: keys}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.hits.Add(1)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.fail != 0 {
			w.WriteHeader(p.fail)
			return
		}
		if p.body != "" {
			_, _ = w.Write([]byte(p.body))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": p.keys})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) set(f func(*fakeProvider)) { p.mu.Lock(); f(p); p.mu.Unlock() }

// clock is a controllable time source shared by the verifier and the tokens.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

const (
	appleAud  = "app.streamnext.ios"
	googleAud = "123-ios.apps.googleusercontent.com"
	rawNonce  = "a-random-raw-nonce"
)

type setup struct {
	v      *Verifier
	apple  *fakeProvider
	google *fakeProvider
	clock  *clock
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	a, _ := testKeys(t)
	s := &setup{apple: newFakeProvider(t, jwkFor("k1", a)), google: newFakeProvider(t, jwkFor("g1", a)), clock: &clock{t: epoch}}
	s.v = New(Config{
		AppleClientID: appleAud, GoogleClientIDs: []string{"other-client", googleAud},
		AppleKeysURL: s.apple.srv.URL, GoogleKeysURL: s.google.srv.URL,
	}, http.DefaultClient, s.clock.Now)
	return s
}

type claimOpt func(jwt.MapClaims)

func appleClaims(opts ...claimOpt) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss": appleIssuer, "aud": appleAud, "sub": "apple-user-1", "exp": epoch.Add(time.Hour).Unix(), "iat": epoch.Unix(),
		"email": "ana@example.com", "email_verified": "true", "nonce": hashNonce(rawNonce),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func googleClaims(opts ...claimOpt) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": googleAud, "sub": "google-user-1", "exp": epoch.Add(time.Hour).Unix(),
		"email": "ana@example.com", "email_verified": true, "name": "Ana Pérez",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func with(k string, v any) claimOpt { return func(c jwt.MapClaims) { c[k] = v } }
func without(k string) claimOpt     { return func(c jwt.MapClaims) { delete(c, k) } }

func sign(t *testing.T, claims jwt.MapClaims, kid string, key *rsa.PrivateKey) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func requireInvalid(t *testing.T, err error, msg ...any) {
	t.Helper()
	assert.ErrorIs(t, err, service.ErrInvalidIdentityToken, msg...)
}

// --- Apple ---------------------------------------------------------------------------------

func TestAppleValidToken(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	id, err := s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(), "k1", a), rawNonce)
	require.NoError(t, err)
	assert.Equal(t, service.Identity{Provider: service.ProviderApple, Subject: "apple-user-1", Email: "ana@example.com", EmailVerified: true}, id)
}

func TestAppleEmailVerifiedMayBeABooleanOrAString(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	for value, want := range map[any]bool{"true": true, true: true, "false": false, false: false} {
		id, err := s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(with("email_verified", value)), "k1", a), rawNonce)
		require.NoError(t, err)
		assert.Equal(t, want, id.EmailVerified, "%v", value)
	}
	id, err := s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(without("email_verified"), without("email")), "k1", a), rawNonce)
	require.NoError(t, err, "Apple omits the email on later sign-ins")
	assert.False(t, id.EmailVerified)
	assert.Empty(t, id.Email)
}

func TestAppleNonceMustMatch(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	token := sign(t, appleClaims(), "k1", a)

	_, err := s.v.Verify(context.Background(), service.ProviderApple, token, "a-different-nonce")
	requireInvalid(t, err, "wrong nonce")
	_, err = s.v.Verify(context.Background(), service.ProviderApple, token, "")
	requireInvalid(t, err, "no nonce sent")
	_, err = s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(with("nonce", hashNonce(""))), "k1", a), "")
	requireInvalid(t, err, "an app must supply a nonce: SHA-256 of nothing is not one")
	_, err = s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(without("nonce")), "k1", a), rawNonce)
	requireInvalid(t, err, "token without a nonce claim")
	_, err = s.v.Verify(context.Background(), service.ProviderApple, sign(t, appleClaims(with("nonce", rawNonce)), "k1", a), rawNonce)
	requireInvalid(t, err, "the claim must be the hash, not the raw nonce")
}

// --- Google --------------------------------------------------------------------------------

func TestGoogleValidTokens(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	for _, issuer := range []string{"https://accounts.google.com", "accounts.google.com"} {
		id, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(with("iss", issuer)), "g1", a), "ignored")
		require.NoError(t, err, issuer)
		assert.Equal(t, service.Identity{Provider: service.ProviderGoogle, Subject: "google-user-1", Email: "ana@example.com", EmailVerified: true, Name: "Ana Pérez"}, id)
	}
}

func TestGoogleNeedsNoNonceAndAcceptsAnyConfiguredClientId(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(with("aud", "other-client")), "g1", a), "")
	assert.NoError(t, err, "the Android client id is configured too")
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(with("aud", []string{"someone-else", googleAud})), "g1", a), "")
	assert.NoError(t, err, "audience as a list")
}

// --- shared rejection rules ----------------------------------------------------------------

func TestTokensThatMustBeRejected(t *testing.T) {
	s := newSetup(t)
	a, b := testKeys(t)
	ctx := context.Background()
	cases := map[string]struct {
		provider service.Provider
		token    string
	}{
		"apple wrong audience":      {service.ProviderApple, sign(t, appleClaims(with("aud", "some.other.app")), "k1", a)},
		"apple wrong issuer":        {service.ProviderApple, sign(t, appleClaims(with("iss", "https://evil.example")), "k1", a)},
		"apple google's issuer":     {service.ProviderApple, sign(t, appleClaims(with("iss", "https://accounts.google.com")), "k1", a)},
		"apple expired":             {service.ProviderApple, sign(t, appleClaims(with("exp", epoch.Add(-time.Minute).Unix())), "k1", a)},
		"apple no expiry":           {service.ProviderApple, sign(t, appleClaims(without("exp")), "k1", a)},
		"apple not yet valid":       {service.ProviderApple, sign(t, appleClaims(with("nbf", epoch.Add(time.Hour).Unix())), "k1", a)},
		"apple no subject":          {service.ProviderApple, sign(t, appleClaims(without("sub")), "k1", a)},
		"apple signed by attacker":  {service.ProviderApple, sign(t, appleClaims(), "k1", b)},
		"apple unknown key id":      {service.ProviderApple, sign(t, appleClaims(), "nope", a)},
		"apple no key id":           {service.ProviderApple, sign(t, appleClaims(), "", a)},
		"google wrong audience":     {service.ProviderGoogle, sign(t, googleClaims(with("aud", "not-ours")), "g1", a)},
		"google wrong issuer":       {service.ProviderGoogle, sign(t, googleClaims(with("iss", "https://appleid.apple.com")), "g1", a)},
		"google expired":            {service.ProviderGoogle, sign(t, googleClaims(with("exp", epoch.Add(-time.Hour).Unix())), "g1", a)},
		"google signed by attacker": {service.ProviderGoogle, sign(t, googleClaims(), "g1", b)},
		"google apple's token":      {service.ProviderGoogle, sign(t, appleClaims(), "g1", a)},
		"empty":                     {service.ProviderGoogle, ""},
		"not a jwt":                 {service.ProviderGoogle, "this is not a token"},
		"two parts":                 {service.ProviderGoogle, "aaa.bbb"},
		"garbage parts":             {service.ProviderGoogle, "aaa.bbb.ccc"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := s.v.Verify(ctx, c.provider, c.token, rawNonce)
			requireInvalid(t, err)
		})
	}
}

func TestOtherSigningAlgorithmsAreRejected(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	ctx := context.Background()

	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, googleClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = s.v.Verify(ctx, service.ProviderGoogle, none, "")
	requireInvalid(t, err, "alg=none")

	// The classic key-confusion attack: an HMAC token whose secret is the (public) key material.
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, googleClaims())
	hs.Header["kid"] = "g1"
	signed, err := hs.SignedString(a.N.Bytes())
	require.NoError(t, err)
	_, err = s.v.Verify(ctx, service.ProviderGoogle, signed, "")
	requireInvalid(t, err, "HS256 with the public key as secret")

	rs512 := jwt.NewWithClaims(jwt.SigningMethodRS512, googleClaims())
	rs512.Header["kid"] = "g1"
	signed, err = rs512.SignedString(a)
	require.NoError(t, err)
	_, err = s.v.Verify(ctx, service.ProviderGoogle, signed, "")
	requireInvalid(t, err, "only RS256 is accepted, even with the right key")
}

func TestClockLeewayIsSmall(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	token := sign(t, googleClaims(with("exp", epoch.Add(-10*time.Second).Unix())), "g1", a)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, token, "")
	assert.NoError(t, err, "10 seconds past expiry is within the 30 second skew allowance")

	s.clock.Advance(time.Minute)
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle, token, "")
	requireInvalid(t, err, "but a minute is not")
}

// --- providers that are not configured -----------------------------------------------------

func TestProviderWithoutAClientIdIsDisabled(t *testing.T) {
	v := New(Config{}, http.DefaultClient, (&clock{t: epoch}).Now)
	for _, p := range []service.Provider{service.ProviderApple, service.ProviderGoogle, "facebook"} {
		_, err := v.Verify(context.Background(), p, "anything", "n")
		assert.ErrorIs(t, err, service.ErrProviderDisabled, p)
	}

	s := newSetup(t)
	_, err := s.v.Verify(context.Background(), "facebook", "x", "")
	assert.ErrorIs(t, err, service.ErrProviderDisabled)
}

func TestDefaultKeyEndpointsAreTheProvidersRealOnes(t *testing.T) {
	v := New(Config{AppleClientID: "a", GoogleClientIDs: []string{"g"}}, http.DefaultClient, (&clock{t: epoch}).Now)
	assert.Equal(t, "https://appleid.apple.com/auth/keys", v.providers[service.ProviderApple].keys.url)
	assert.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", v.providers[service.ProviderGoogle].keys.url)
}
