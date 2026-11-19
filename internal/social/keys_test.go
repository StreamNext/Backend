package social

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func TestKeysAreCachedForAnHour(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	token := sign(t, googleClaims(), "g1", a)
	verify := func() error {
		_, err := s.v.Verify(context.Background(), service.ProviderGoogle, token, "")
		return err
	}

	require.NoError(t, verify())
	require.NoError(t, verify())
	assert.EqualValues(t, 1, s.google.hits.Load(), "the second verification used the cache")

	s.clock.Advance(keysTTL - time.Minute)
	require.NoError(t, verify())
	assert.EqualValues(t, 1, s.google.hits.Load(), "still inside the hour")

	s.clock.Advance(2 * time.Minute)
	fresh := sign(t, googleClaims(with("exp", s.clock.Now().Add(time.Hour).Unix())), "g1", a)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, fresh, "")
	require.NoError(t, err)
	assert.EqualValues(t, 2, s.google.hits.Load(), "refetched after the TTL")
}

func TestKeyRotationIsPickedUpWithoutWaitingForTheTTL(t *testing.T) {
	s := newSetup(t)
	a, b := testKeys(t)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	require.NoError(t, err)

	// Google rotates: a new key id appears, signed with another key.
	s.google.set(func(p *fakeProvider) { p.keys = []map[string]string{jwkFor("g1", a), jwkFor("g2", b)} })
	s.clock.Advance(2 * time.Minute) // past minRefetch
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(with("exp", s.clock.Now().Add(time.Hour).Unix())), "g2", b), "")
	require.NoError(t, err, "an unknown key id triggers one refetch")
	assert.EqualValues(t, 2, s.google.hits.Load())
}

func TestUnknownKeyIdsCannotMakeUsHammerTheProvider(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	require.NoError(t, err)
	base := s.google.hits.Load()

	for i := 0; i < 20; i++ {
		_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "forged-kid", a), "")
		requireInvalid(t, err)
	}
	assert.LessOrEqual(t, s.google.hits.Load()-base, int32(1), "at most one refetch inside the rate-limit window")

	s.clock.Advance(2 * time.Minute)
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(with("exp", s.clock.Now().Add(time.Hour).Unix())), "forged-kid", a), "")
	requireInvalid(t, err)
	assert.LessOrEqual(t, s.google.hits.Load()-base, int32(2), "and again only after a minute")
}

func TestProviderOutageWithNothingCachedIsUnavailableNotInvalid(t *testing.T) {
	a, _ := testKeys(t)
	for name, break_ := range map[string]func(*fakeProvider){
		"server error": func(p *fakeProvider) { p.fail = http.StatusInternalServerError },
		"not json":     func(p *fakeProvider) { p.body = "<html>oops</html>" },
		"no keys":      func(p *fakeProvider) { p.body = `{"keys":[]}` },
		"only unusable": func(p *fakeProvider) {
			p.body = `{"keys":[{"kty":"EC","kid":"x"},{"kty":"RSA","kid":"","n":"AQAB","e":"AQAB"},{"kty":"RSA","kid":"bad","n":"!!","e":"AQAB"}]}`
		},
		"exponent too big": func(p *fakeProvider) { p.body = `{"keys":[{"kty":"RSA","kid":"x","n":"AQAB","e":"AQABAQABAQAB"}]}` },
		"exponent too low": func(p *fakeProvider) { p.body = `{"keys":[{"kty":"RSA","kid":"x","n":"AQAB","e":"AQ"}]}` },
	} {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			s.google.set(break_)
			_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
			assert.ErrorIs(t, err, service.ErrVerifierUnavailable)
			assert.NotErrorIs(t, err, service.ErrInvalidIdentityToken, "a provider outage must not look like a bad token")
		})
	}
}

func TestUnreachableProviderIsUnavailable(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	s.google.srv.Close()
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	assert.ErrorIs(t, err, service.ErrVerifierUnavailable)
}

func TestCachedKeysKeepWorkingThroughAShortOutage(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	require.NoError(t, err)

	s.google.set(func(p *fakeProvider) { p.fail = http.StatusServiceUnavailable })
	s.clock.Advance(keysTTL + time.Minute) // the cache is stale and the refresh will fail
	token := sign(t, googleClaims(with("exp", s.clock.Now().Add(time.Hour).Unix())), "g1", a)
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle, token, "")
	assert.NoError(t, err, "stale keys are better than refusing every sign-in")
}

func TestAFailedRefreshForAnUnknownKeyIdIsUnavailable(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	_, err := s.v.Verify(context.Background(), service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	require.NoError(t, err)

	s.google.set(func(p *fakeProvider) { p.fail = http.StatusBadGateway })
	s.clock.Advance(2 * time.Minute)
	_, err = s.v.Verify(context.Background(), service.ProviderGoogle,
		sign(t, googleClaims(with("exp", s.clock.Now().Add(time.Hour).Unix())), "brand-new-kid", a), "")
	assert.ErrorIs(t, err, service.ErrVerifierUnavailable)
}

func TestContextCancellationStopsAKeyFetch(t *testing.T) {
	s := newSetup(t)
	a, _ := testKeys(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.v.Verify(ctx, service.ProviderGoogle, sign(t, googleClaims(), "g1", a), "")
	assert.ErrorIs(t, err, service.ErrVerifierUnavailable)
}

func TestBadKeyEndpointURL(t *testing.T) {
	k := newKeySet("http://bad host", http.DefaultClient, (&clock{t: epoch}).Now)
	_, err := k.key(context.Background(), "x")
	assert.ErrorIs(t, err, service.ErrVerifierUnavailable)
}
