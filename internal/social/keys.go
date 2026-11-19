// Package social verifies Apple and Google ID tokens: it fetches and caches the providers' public
// signing keys (keys.go) and checks a token against them (verifier.go).
package social

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/StreamNext/Backend/internal/service"
)

const (
	keysTTL = time.Hour
	// minRefetch stops tokens carrying unknown key ids from making us hammer the provider.
	minRefetch = time.Minute
	maxKeysDoc = 1 << 20
)

var errUnknownKey = errors.New("no signing key with that id")

// keySet fetches and caches a provider's public keys (a JWKS document).
type keySet struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time // last successful fetch, for the TTL
	lastAttempt time.Time // last fetch of any outcome, to rate-limit refetches for unknown key ids
}

func newKeySet(url string, client *http.Client, now func() time.Time) *keySet {
	return &keySet{url: url, client: client, now: now}
}

// key returns the signing key with the given id. The cache is refreshed after the TTL, and once more
// (at most every minRefetch) for an unknown id, because providers rotate keys. If a refresh fails while
// keys are cached, the cached keys keep working, so a short provider outage doesn't stop sign-in.
func (k *keySet) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()

	if k.keys == nil || now.Sub(k.fetchedAt) > keysTTL {
		if err := k.fetch(ctx, now); err != nil && k.keys == nil {
			return nil, err
		}
	}
	if key, ok := k.keys[kid]; ok {
		return key, nil
	}
	if now.Sub(k.lastAttempt) >= minRefetch {
		if err := k.fetch(ctx, now); err != nil {
			return nil, err
		}
		if key, ok := k.keys[kid]; ok {
			return key, nil
		}
	}
	return nil, errUnknownKey
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetch replaces the cache with the provider's current keys. Every failure is ErrVerifierUnavailable.
func (k *keySet) fetch(ctx context.Context, now time.Time) error {
	k.lastAttempt = now
	unavailable := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{service.ErrVerifierUnavailable}, args...)...)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return unavailable("%v", err)
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return unavailable("fetch keys: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return unavailable("fetch keys: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxKeysDoc)).Decode(&doc); err != nil {
		return unavailable("keys document: %v", err)
	}

	keys := map[string]*rsa.PublicKey{}
	for _, j := range doc.Keys {
		if key, ok := j.rsaKey(); ok {
			keys[j.Kid] = key
		}
	}
	if len(keys) == 0 { // never replace a working cache with nothing
		return unavailable("keys document had no usable RSA keys")
	}
	k.keys, k.fetchedAt = keys, now
	return nil
}

func (j jwk) rsaKey() (*rsa.PublicKey, bool) {
	if j.Kty != "RSA" || j.Kid == "" {
		return nil, false
	}
	n, errN := base64.RawURLEncoding.DecodeString(j.N)
	e, errE := base64.RawURLEncoding.DecodeString(j.E)
	if errN != nil || errE != nil || len(n) == 0 || len(e) == 0 || len(e) > 4 {
		return nil, false
	}
	exp := int(new(big.Int).SetBytes(e).Int64())
	if exp < 3 {
		return nil, false
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}, true
}
