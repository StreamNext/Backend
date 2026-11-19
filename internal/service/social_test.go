package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/auth"
)

type socialEnv struct {
	*flows
	oauth    *fakeOAuth
	verifier *fakeVerifier
	social   *SocialAuth
}

func newSocial(t *testing.T) *socialEnv {
	t.Helper()
	f := newFlows(t)
	s := &socialEnv{flows: f, oauth: newFakeOAuth(), verifier: &fakeVerifier{identity: Identity{Subject: "sub-1", Email: "ana@example.com", EmailVerified: true, Name: "Ana Pérez"}}}
	s.social = NewSocialAuth(SocialDeps{Users: f.users, OAuth: s.oauth, Sessions: f.tokens, Verifier: s.verifier, Auth: f.svc,
		Verification: f.verification, Now: f.clock.Now, Log: zap.NewNop()})
	return s
}

func (s *socialEnv) signIn(provider Provider) (Session, error) {
	return s.social.SignIn(s.ctx, SocialInput{Provider: provider, Token: "id-token", Nonce: "raw-nonce", DeviceID: device})
}

func (s *socialEnv) userByEmail(t *testing.T, email string) User {
	t.Helper()
	u, err := s.users.GetByEmail(s.ctx, email)
	require.NoError(t, err)
	return u
}

func TestNewGoogleUserIsCreatedVerifiedWithoutAPassword(t *testing.T) {
	s := newSocial(t)
	sess, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)

	assert.Equal(t, "ana@example.com", sess.User.Email)
	assert.Equal(t, "Ana Pérez", sess.User.DisplayName, "from the provider's name claim")
	assert.Nil(t, sess.User.PasswordHash)
	assert.NotNil(t, sess.User.EmailVerifiedAt, "the provider verified the address")
	assert.Equal(t, "UTC", sess.User.Timezone)
	assert.Equal(t, "id-token", s.verifier.gotToken)
	assert.Equal(t, "raw-nonce", s.verifier.gotNonce)
	assert.Equal(t, ProviderGoogle, s.verifier.gotProvider)
	assert.Empty(t, s.mailer.sent, "no verification email: it is already verified")

	stored, err := s.tokens.GetByHash(s.ctx, auth.HashToken(sess.RefreshToken))
	require.NoError(t, err)
	assert.Equal(t, device, stored.DeviceID, "the session is bound to the device")
	assert.Equal(t, epoch.Add(90*24*time.Hour), stored.ExpiresAt, "provider sign-ins always last 90 days")
}

func TestDisplayNameFallbacks(t *testing.T) {
	s := newSocial(t)
	s.verifier.identity.Name = ""
	sess, err := s.social.SignIn(s.ctx, SocialInput{Provider: ProviderApple, Token: "t", Nonce: "n", DeviceID: device, DisplayName: "  Ana from the app "})
	require.NoError(t, err)
	assert.Equal(t, "Ana from the app", sess.User.DisplayName, "Apple only shares the name through the client")

	s2 := newSocial(t)
	s2.verifier.identity.Name = ""
	sess, err = s2.signIn(ProviderApple)
	require.NoError(t, err)
	assert.Equal(t, "ana", sess.User.DisplayName, "then the part of the email before the @")

	assert.Equal(t, strings.Repeat("a", 40), providerDisplayName("x@y.co", strings.Repeat("a", 60)), "cut, not rejected")
	assert.Equal(t, "ñ", providerDisplayName("x@y.co", "", "  ", "ñ"))
}

func TestAKnownIdentitySignsInTheSameUserWhateverTheEmailSaysNow(t *testing.T) {
	s := newSocial(t)
	first, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)

	s.verifier.identity.Email = "a-new-address@example.com" // the person changed their Google email
	again, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)
	assert.Equal(t, first.User.ID, again.User.ID)
	assert.Len(t, s.users.byID, 1, "no second account")
	assert.Equal(t, "ana@example.com", again.User.Email, "our record is unchanged")
}

func TestAProviderIdentityIsPerProvider(t *testing.T) {
	s := newSocial(t)
	g, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)
	a, err := s.signIn(ProviderApple) // same subject string, other provider, same verified email
	require.NoError(t, err)
	assert.Equal(t, g.User.ID, a.User.ID, "the verified email links the second provider to the same account")
	assert.Len(t, s.oauth.links, 2)
}

func TestLinkingToAVerifiedAccountKeepsItsPassword(t *testing.T) {
	s := newSocial(t)
	reg := s.register(t, "ana@example.com", device)
	require.NoError(t, s.verification.Confirm(s.ctx, s.mailer.last().token()))

	sess, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)
	assert.Equal(t, reg.User.ID, sess.User.ID)
	assert.NotNil(t, sess.User.PasswordHash, "the owner already proved the email, so the password stays")
	assert.True(t, s.loginWorks("ana@example.com", "correct-horse", otherDevice))
	assert.Len(t, s.users.byID, 1)
}

func TestLinkingToAnUnverifiedAccountTakesItAwayFromWhoeverRegisteredIt(t *testing.T) {
	s := newSocial(t)
	// An attacker registered the victim's address and knows the password. It was never verified.
	attacker := s.register(t, "ana@example.com", device)
	require.Nil(t, s.userByEmail(t, "ana@example.com").EmailVerifiedAt)
	require.True(t, s.loginWorks("ana@example.com", "correct-horse", otherDevice))

	// The real owner signs in with Google.
	sess, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)

	assert.Equal(t, attacker.User.ID, sess.User.ID, "same account, now in the owner's hands")
	assert.Nil(t, s.userByEmail(t, "ana@example.com").PasswordHash, "the attacker's password is gone")
	assert.False(t, s.loginWorks("ana@example.com", "correct-horse", otherDevice), "the attacker can no longer log in")
	_, err = s.svc.Refresh(s.ctx, RefreshInput{RefreshToken: attacker.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated, "the attacker's session was revoked")
	assert.NotNil(t, s.userByEmail(t, "ana@example.com").EmailVerifiedAt, "and the email is now verified")
	assert.Len(t, s.tokens.live(sess.User.ID), 1, "only the owner's new session is live")
}

func TestAnUnverifiedProviderEmailNeverLinks(t *testing.T) {
	s := newSocial(t)
	reg := s.register(t, "ana@example.com", device)
	s.verifier.identity.EmailVerified = false

	_, err := s.signIn(ProviderGoogle)
	requireKind(t, err, apperr.Conflict)
	assert.Empty(t, s.oauth.links, "nothing was linked")
	assert.NotNil(t, s.userByEmail(t, "ana@example.com").PasswordHash, "and the account is untouched")
	assert.Len(t, s.tokens.live(reg.User.ID), 1)
}

func TestAnUnverifiedProviderEmailForANewAddressCreatesAnUnverifiedUser(t *testing.T) {
	s := newSocial(t)
	s.verifier.identity.EmailVerified = false

	sess, err := s.signIn(ProviderGoogle)
	require.NoError(t, err)
	assert.Nil(t, sess.User.EmailVerifiedAt)
	require.Len(t, s.mailer.sent, 1, "so they get the normal verification email")
	assert.Equal(t, "verify", s.mailer.sent[0].kind)
}

func TestAMailFailureNeverBlocksAProviderSignUp(t *testing.T) {
	s := newSocial(t)
	s.verifier.identity.EmailVerified = false
	s.mailer.err = errors.New("provider down")
	_, err := s.signIn(ProviderGoogle)
	assert.NoError(t, err)

	t2 := newSocial(t)
	t2.verifier.identity.EmailVerified = false
	t2.social.Verification = nil
	_, err = t2.signIn(ProviderGoogle)
	assert.NoError(t, err, "no verification sender configured")
}

func TestWithoutAUsableEmailThereIsNoAccount(t *testing.T) {
	for _, email := range []string{"", "not an email", "Ana <a@b.co>"} {
		s := newSocial(t)
		s.verifier.identity.Email = email
		_, err := s.signIn(ProviderApple)
		requireKind(t, err, apperr.Unauthenticated, email)
		assert.Empty(t, s.users.byID)
	}
}

func TestVerifierFailuresMapToTheRightStatus(t *testing.T) {
	cases := map[string]struct {
		err  error
		kind apperr.Kind
	}{
		"provider not configured": {ErrProviderDisabled, apperr.Unavailable},
		"bad token":               {errors.Join(ErrInvalidIdentityToken, errors.New("expired")), apperr.Unauthenticated},
		"provider keys down":      {ErrVerifierUnavailable, apperr.Unavailable},
		"unexpected":              {errors.New("boom"), apperr.Internal},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSocial(t)
			s.verifier.err = c.err
			_, err := s.signIn(ProviderGoogle)
			requireKind(t, err, c.kind)
			assert.Empty(t, s.users.byID)
		})
	}
}

func TestADeviceIsRequired(t *testing.T) {
	s := newSocial(t)
	_, err := s.social.SignIn(s.ctx, SocialInput{Provider: ProviderGoogle, Token: "t"})
	requireKind(t, err, apperr.InvalidArgument)
	assert.Empty(t, s.verifier.gotToken, "the token isn't even checked")
}

func TestTwoFirstSignInsAtOnceEndUpWithOneAccount(t *testing.T) {
	s := newSocial(t)
	// Another request creates the user and links the identity between our checks and our Create.
	s.users.beforeCreate = func() {
		u, err := s.users.Create(s.ctx, NewUser{Email: "ana@example.com", DisplayName: "Ana"})
		require.NoError(t, err)
		require.NoError(t, s.oauth.Link(s.ctx, u.ID, Identity{Provider: ProviderGoogle, Subject: "sub-1"}, epoch))
	}
	sess, err := s.signIn(ProviderGoogle)
	require.NoError(t, err, "the loser re-reads and finds what the winner made")
	assert.Len(t, s.users.byID, 1)
	assert.Equal(t, "ana@example.com", sess.User.Email)
}

func TestRetryGivesUpAfterASecondConflict(t *testing.T) {
	s := newSocial(t)
	s.users.fail["Create"] = ErrEmailTaken
	_, err := s.signIn(ProviderGoogle)
	requireKind(t, err, apperr.Internal)
}

func TestSocialInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	steps := map[string]func(*socialEnv){
		"find identity": func(s *socialEnv) { s.oauth.fail["FindUser"] = boom },
		"find by email": func(s *socialEnv) { s.users.fail["GetByEmail"] = boom },
		"create":        func(s *socialEnv) { s.users.fail["Create"] = boom },
		"link new user": func(s *socialEnv) { s.oauth.fail["Link"] = boom },
		"issue session": func(s *socialEnv) { s.tokens.fail["Issue"] = boom },
	}
	for name, setup := range steps {
		t.Run(name, func(t *testing.T) {
			s := newSocial(t)
			setup(s)
			_, err := s.signIn(ProviderGoogle)
			requireKind(t, err, apperr.Internal)
		})
	}

	linked := map[string]func(*socialEnv){
		"load linked user": func(s *socialEnv) { s.users.fail["GetByID"] = boom },
	}
	for name, setup := range linked {
		t.Run(name, func(t *testing.T) {
			s := newSocial(t)
			_, err := s.signIn(ProviderGoogle)
			require.NoError(t, err)
			setup(s)
			_, err = s.signIn(ProviderGoogle)
			requireKind(t, err, apperr.Internal)
		})
	}

	takeover := map[string]func(*socialEnv){
		"clear password":  func(s *socialEnv) { s.users.fail["ClearPassword"] = boom },
		"revoke sessions": func(s *socialEnv) { s.tokens.fail["RevokeAllForUser"] = boom },
		"link existing":   func(s *socialEnv) { s.oauth.fail["Link"] = boom },
		"mark verified":   func(s *socialEnv) { s.users.fail["MarkEmailVerified"] = boom },
	}
	for name, setup := range takeover {
		t.Run(name, func(t *testing.T) {
			s := newSocial(t)
			s.register(t, "ana@example.com", device) // unverified, so linking takes the account over
			setup(s)
			_, err := s.signIn(ProviderGoogle)
			requireKind(t, err, apperr.Internal)
		})
	}

	t.Run("reload after linking", func(t *testing.T) {
		s := newSocial(t)
		s.register(t, "ana@example.com", device)
		s.users.fail["GetByID"] = boom
		_, err := s.signIn(ProviderGoogle)
		requireKind(t, err, apperr.Internal)
	})
}
