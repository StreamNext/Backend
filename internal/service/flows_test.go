package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

const baseURL = "https://app.test"

// flows wires the email and account services around the same fakes as the auth tests.
type flows struct {
	*env
	emails       *fakeEmailTokens
	mailer       *fakeMailer
	verification *Verification
	reset        *PasswordReset
	account      *Account
}

func newFlows(t *testing.T) *flows {
	t.Helper()
	e := newEnv(10 * time.Second)
	f := &flows{env: e, emails: newFakeEmailTokens(), mailer: &fakeMailer{}}
	f.verification = NewVerification(VerificationDeps{Users: e.users, Tokens: f.emails, Mailer: f.mailer, BaseURL: baseURL, Now: e.clock.Now, Log: zap.NewNop()})
	f.reset = NewPasswordReset(PasswordResetDeps{Users: e.users, Sessions: e.tokens, Tokens: f.emails, Mailer: f.mailer, Hasher: e.hasher, BaseURL: baseURL, Now: e.clock.Now, Log: zap.NewNop()})
	f.account = NewAccount(AccountDeps{Users: e.users, Sessions: e.tokens, Hasher: e.hasher, Auth: e.svc, Now: e.clock.Now, Log: zap.NewNop()})
	e.svc.Verifier = f.verification
	return f
}

func (f *flows) loginWorks(email, password string, dev uuid.UUID) bool {
	_, err := f.svc.Login(f.ctx, LoginInput{Email: email, Password: password, DeviceID: dev})
	return err == nil
}

// --- verification --------------------------------------------------------------------------

func TestRegisterSendsAVerificationEmail(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)

	require.Len(t, f.mailer.sent, 1)
	m := f.mailer.sent[0]
	assert.Equal(t, "verify", m.kind)
	assert.Equal(t, "ana@example.com", m.to)
	assert.Equal(t, "ana", m.name)
	assert.Contains(t, m.link, baseURL+"/verify-email?token=")
	assert.NotEmpty(t, m.token())
	assert.Nil(t, s.User.EmailVerifiedAt)
}

func TestRegisterSucceedsEvenIfTheEmailCannotBeSent(t *testing.T) {
	f := newFlows(t)
	f.mailer.err = errors.New("provider down")
	_, err := f.svc.Register(f.ctx, RegisterInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: device})
	require.NoError(t, err, "a mail failure must never block sign-up")
	assert.Len(t, f.mailer.sent, 1, "it was attempted")
}

func TestRegisterWithoutAVerifierSendsNothing(t *testing.T) {
	e := newEnv(10 * time.Second) // no Verifier configured
	e.register(t, "ana@example.com", device)
}

func TestConfirmVerifiesTheEmailOnce(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	token := f.mailer.last().token()

	require.NoError(t, f.verification.Confirm(f.ctx, token))
	assert.NotNil(t, f.users.byID[s.User.ID].EmailVerifiedAt)

	requireKind(t, f.verification.Confirm(f.ctx, token), apperr.InvalidArgument) // single-use
}

func TestConfirmRejectsUnknownExpiredAndWrongPurposeTokens(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	token := f.mailer.last().token()

	requireKind(t, f.verification.Confirm(f.ctx, "not-a-token"), apperr.InvalidArgument)
	requireKind(t, f.verification.Confirm(f.ctx, ""), apperr.InvalidArgument)

	f.clock.Advance(24*time.Hour + time.Second)
	requireKind(t, f.verification.Confirm(f.ctx, token), apperr.InvalidArgument, "expired")
	assert.Nil(t, f.users.byID[s.User.ID].EmailVerifiedAt)

	// A reset token is not a verification token, and the other way round.
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	resetToken := f.mailer.last().token()
	requireKind(t, f.verification.Confirm(f.ctx, resetToken), apperr.InvalidArgument)
	requireKind(t, f.reset.Reset(f.ctx, token, "a-new-password-1"), apperr.InvalidArgument)
}

func TestConfirmForADeletedAccount(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	token := f.mailer.last().token()
	delete(f.users.byID, s.User.ID)
	requireKind(t, f.verification.Confirm(f.ctx, token), apperr.InvalidArgument)
}

func TestResendSendsANewLinkAndKillsTheOldOne(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	first := f.mailer.last().token()

	f.clock.Advance(emailCooldown + time.Second)
	require.NoError(t, f.verification.Resend(f.ctx, " ANA@example.com "))
	require.Len(t, f.mailer.sent, 2)
	second := f.mailer.last().token()
	assert.NotEqual(t, first, second)

	requireKind(t, f.verification.Confirm(f.ctx, first), apperr.InvalidArgument, "superseded")
	require.NoError(t, f.verification.Confirm(f.ctx, second))
}

func TestResendRespectsTheCooldown(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)

	f.clock.Advance(emailCooldown - time.Second)
	require.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"))
	assert.Len(t, f.mailer.sent, 1, "inside the cooldown nothing is sent, and the answer is the same")

	f.clock.Advance(2 * time.Second)
	require.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"))
	assert.Len(t, f.mailer.sent, 2)
}

func TestResendGivesNothingAway(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	require.NoError(t, f.verification.Confirm(f.ctx, f.mailer.last().token()))
	f.mailer.sent = nil
	f.clock.Advance(time.Hour)

	assert.NoError(t, f.verification.Resend(f.ctx, "nobody@example.com"), "unknown address")
	assert.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"), "already verified")
	assert.Empty(t, f.mailer.sent)
	assert.NotNil(t, f.users.byID[s.User.ID].EmailVerifiedAt)

	requireKind(t, f.verification.Resend(f.ctx, "not an email"), apperr.InvalidArgument)
}

func TestVerificationInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	token := f.mailer.last().token()
	f.clock.Advance(emailCooldown + time.Second)

	f.users.fail["GetByEmail"] = boom
	requireKind(t, f.verification.Resend(f.ctx, "ana@example.com"), apperr.Internal)
	delete(f.users.fail, "GetByEmail")

	// Sending problems are logged, never shown: the answer must not depend on whether the account exists.
	f.emails.fail["LastCreated"] = boom
	assert.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"))
	delete(f.emails.fail, "LastCreated")
	f.emails.fail["Replace"] = boom
	assert.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"))
	delete(f.emails.fail, "Replace")
	f.mailer.err = boom
	assert.NoError(t, f.verification.Resend(f.ctx, "ana@example.com"))
	f.mailer.err = nil
	token = f.mailer.last().token() // that last Resend issued a newer token, which superseded the first

	f.emails.fail["Consume"] = boom
	requireKind(t, f.verification.Confirm(f.ctx, token), apperr.Internal)
	delete(f.emails.fail, "Consume")
	f.users.fail["MarkEmailVerified"] = boom
	requireKind(t, f.verification.Confirm(f.ctx, token), apperr.Internal)
}

// --- password reset ------------------------------------------------------------------------

func TestForgotPasswordEmailsAResetLink(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	f.mailer.sent = nil

	require.NoError(t, f.reset.Request(f.ctx, "Ana@Example.com"))
	require.Len(t, f.mailer.sent, 1)
	m := f.mailer.sent[0]
	assert.Equal(t, "reset", m.kind)
	assert.Equal(t, "ana@example.com", m.to)
	assert.Contains(t, m.link, baseURL+"/reset-password?token=")
}

func TestForgotPasswordGivesNothingAway(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	social := User{ID: uuid.New(), Email: "social@example.com", DisplayName: "S"} // no password
	f.users.byID[social.ID] = social
	f.mailer.sent = nil

	assert.NoError(t, f.reset.Request(f.ctx, "nobody@example.com"))
	assert.NoError(t, f.reset.Request(f.ctx, "social@example.com"), "accounts without a password get no reset email")
	assert.Empty(t, f.mailer.sent)
	requireKind(t, f.reset.Request(f.ctx, "nope"), apperr.InvalidArgument)
}

func TestForgotPasswordRespectsTheCooldownAndSupersedes(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	f.mailer.sent = nil

	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	first := f.mailer.last().token()
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	assert.Len(t, f.mailer.sent, 1, "second request inside the cooldown sends nothing")

	f.clock.Advance(emailCooldown + time.Second)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	require.Len(t, f.mailer.sent, 2)
	requireKind(t, f.reset.Reset(f.ctx, first, "a-new-password-1"), apperr.InvalidArgument, "the older link was superseded")
	require.NoError(t, f.reset.Reset(f.ctx, f.mailer.last().token(), "a-new-password-1"))
}

func TestResetChangesThePasswordAndSignsOutEverywhere(t *testing.T) {
	f := newFlows(t)
	phone := f.register(t, "ana@example.com", device)
	_, err := f.svc.Login(f.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: otherDevice})
	require.NoError(t, err)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	token := f.mailer.last().token()

	require.NoError(t, f.reset.Reset(f.ctx, token, "a-brand-new-pass"))

	assert.False(t, f.loginWorks("ana@example.com", "correct-horse", device), "old password no longer works")
	assert.True(t, f.loginWorks("ana@example.com", "a-brand-new-pass", device))
	_, err = f.svc.Refresh(f.ctx, RefreshInput{RefreshToken: phone.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated, "existing sessions were revoked")

	requireKind(t, f.reset.Reset(f.ctx, token, "yet-another-pass-1"), apperr.InvalidArgument, "single-use")
}

func TestAStaleDeviceAfterAResetCannotSignTheUserOutAgain(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	tablet, err := f.svc.Login(f.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: otherDevice})
	require.NoError(t, err)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	require.NoError(t, f.reset.Reset(f.ctx, f.mailer.last().token(), "a-brand-new-pass"))
	fresh, err := f.svc.Login(f.ctx, LoginInput{Email: "ana@example.com", Password: "a-brand-new-pass", DeviceID: device})
	require.NoError(t, err)

	// The tablet still holds its old token and tries it.
	_, err = f.svc.Refresh(f.ctx, RefreshInput{RefreshToken: tablet.RefreshToken, DeviceID: otherDevice})
	requireKind(t, err, apperr.Unauthenticated)

	_, err = f.svc.Refresh(f.ctx, RefreshInput{RefreshToken: fresh.RefreshToken, DeviceID: device})
	assert.NoError(t, err, "the session created after the reset survives")
}

func TestResetRevokesEverySession(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	_, err := f.svc.Login(f.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: otherDevice})
	require.NoError(t, err)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	require.Len(t, f.tokens.live(s.User.ID), 2)

	require.NoError(t, f.reset.Reset(f.ctx, f.mailer.last().token(), "a-brand-new-pass"))
	assert.Empty(t, f.tokens.live(s.User.ID))
}

func TestResetWithAWeakPasswordKeepsTheLinkUsable(t *testing.T) {
	f := newFlows(t)
	f.register(t, "ana@example.com", device)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	token := f.mailer.last().token()

	requireKind(t, f.reset.Reset(f.ctx, token, "short"), apperr.InvalidArgument)
	require.NoError(t, f.reset.Reset(f.ctx, token, "a-long-enough-password"), "the same link still works")
}

func TestResetRejectsBadExpiredAndOrphanedTokens(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	token := f.mailer.last().token()

	requireKind(t, f.reset.Reset(f.ctx, "garbage", "a-long-enough-password"), apperr.InvalidArgument)

	f.clock.Advance(resetTokenTTL + time.Second)
	requireKind(t, f.reset.Reset(f.ctx, token, "a-long-enough-password"), apperr.InvalidArgument, "reset links last one hour")

	f.clock.Advance(emailCooldown)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	orphaned := f.mailer.last().token()
	delete(f.users.byID, s.User.ID)
	requireKind(t, f.reset.Reset(f.ctx, orphaned, "a-long-enough-password"), apperr.InvalidArgument)
}

func TestPasswordResetInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	f := newFlows(t)
	f.register(t, "ana@example.com", device)

	f.users.fail["GetByEmail"] = boom
	requireKind(t, f.reset.Request(f.ctx, "ana@example.com"), apperr.Internal)
	delete(f.users.fail, "GetByEmail")
	f.emails.fail["LastCreated"] = boom
	requireKind(t, f.reset.Request(f.ctx, "ana@example.com"), apperr.Internal)
	delete(f.emails.fail, "LastCreated")
	f.mailer.err = boom
	assert.NoError(t, f.reset.Request(f.ctx, "ana@example.com"), "a provider failure is logged, not shown")
	f.mailer.err = nil

	f.clock.Advance(emailCooldown + time.Second)
	require.NoError(t, f.reset.Request(f.ctx, "ana@example.com"))
	token := f.mailer.last().token()
	pw := "a-long-enough-password"

	f.emails.fail["Consume"] = boom
	requireKind(t, f.reset.Reset(f.ctx, token, pw), apperr.Internal)
	delete(f.emails.fail, "Consume")

	for _, c := range []struct {
		name   string
		breaks func(*flows)
	}{
		{"hashing", func(g *flows) { g.reset.Hasher = hashFails{&fakeHasher{}} }},
		{"setting the password", func(g *flows) { g.users.fail["SetPassword"] = boom }},
		{"revoking sessions", func(g *flows) { g.tokens.fail["RevokeAllForUser"] = boom }},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newFlows(t)
			g.register(t, "bob@example.com", device)
			require.NoError(t, g.reset.Request(g.ctx, "bob@example.com"))
			c.breaks(g)
			requireKind(t, g.reset.Reset(g.ctx, g.mailer.last().token(), pw), apperr.Internal)
		})
	}
}

// --- change password -----------------------------------------------------------------------

func TestChangePasswordSwitchesPasswordAndSessions(t *testing.T) {
	f := newFlows(t)
	phone := f.register(t, "ana@example.com", device)
	tablet, err := f.svc.Login(f.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: otherDevice})
	require.NoError(t, err)

	s, err := f.account.ChangePassword(f.ctx, phone.User.ID, ChangePasswordInput{CurrentPassword: "correct-horse", NewPassword: "a-brand-new-pass", DeviceID: device})
	require.NoError(t, err)

	assert.NotEmpty(t, s.AccessToken)
	assert.Equal(t, phone.User.ID, s.User.ID)
	assert.Len(t, f.tokens.live(phone.User.ID), 1, "only the new session for the requesting device is live")
	_, err = f.svc.Refresh(f.ctx, RefreshInput{RefreshToken: tablet.RefreshToken, DeviceID: otherDevice})
	requireKind(t, err, apperr.Unauthenticated, "other devices are signed out")
	got, err := f.svc.Refresh(f.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err, "the returned session works")
	assert.NotEmpty(t, got.RefreshToken)

	assert.False(t, f.loginWorks("ana@example.com", "correct-horse", otherDevice))
	assert.True(t, f.loginWorks("ana@example.com", "a-brand-new-pass", otherDevice))
}

func TestChangePasswordHonoursRememberMe(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	out, err := f.account.ChangePassword(f.ctx, s.User.ID, ChangePasswordInput{CurrentPassword: "correct-horse", NewPassword: "a-brand-new-pass", DeviceID: device, RememberMe: true})
	require.NoError(t, err)
	live := f.tokens.live(s.User.ID)
	require.Len(t, live, 1)
	assert.Equal(t, epoch.Add(90*24*time.Hour), live[0].ExpiresAt)
	assert.NotEmpty(t, out.RefreshToken)
}

func TestChangePasswordWithTheWrongCurrentPasswordChangesNothing(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)

	_, err := f.account.ChangePassword(f.ctx, s.User.ID, ChangePasswordInput{CurrentPassword: "not-my-password", NewPassword: "a-brand-new-pass", DeviceID: device})
	requireKind(t, err, apperr.Forbidden)
	assert.Len(t, f.tokens.live(s.User.ID), 1, "sessions untouched")
	assert.True(t, f.loginWorks("ana@example.com", "correct-horse", device), "password untouched")
}

func TestChangePasswordRejectsBadInput(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	social := User{ID: uuid.New(), Email: "social@example.com"}
	f.users.byID[social.ID] = social
	ok := ChangePasswordInput{CurrentPassword: "correct-horse", NewPassword: "a-brand-new-pass", DeviceID: device}

	weak := ok
	weak.NewPassword = "short"
	_, err := f.account.ChangePassword(f.ctx, s.User.ID, weak)
	requireKind(t, err, apperr.InvalidArgument)

	noDevice := ok
	noDevice.DeviceID = uuid.Nil
	_, err = f.account.ChangePassword(f.ctx, s.User.ID, noDevice)
	requireKind(t, err, apperr.InvalidArgument)

	_, err = f.account.ChangePassword(f.ctx, social.ID, ok)
	requireKind(t, err, apperr.InvalidArgument)
	assert.ErrorContains(t, err, "no password")

	_, err = f.account.ChangePassword(f.ctx, uuid.New(), ok)
	requireKind(t, err, apperr.Unauthenticated)
}

func TestChangePasswordInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	in := ChangePasswordInput{CurrentPassword: "correct-horse", NewPassword: "a-brand-new-pass", DeviceID: device}
	for name, setup := range map[string]func(*flows){
		"find user":   func(f *flows) { f.users.fail["GetByID"] = boom },
		"verify":      func(f *flows) { f.account.Hasher = errHasher{&fakeHasher{}} },
		"hash":        func(f *flows) { f.account.Hasher = hashFails{&fakeHasher{}} },
		"set":         func(f *flows) { f.users.fail["SetPassword"] = boom },
		"revoke all":  func(f *flows) { f.tokens.fail["RevokeAllForUser"] = boom },
		"new session": func(f *flows) { f.svc.Access = fakeAccess{fail: boom} },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFlows(t)
			s := f.register(t, "ana@example.com", device)
			setup(f)
			_, err := f.account.ChangePassword(f.ctx, s.User.ID, in)
			requireKind(t, err, apperr.Internal)
		})
	}
}

// --- delete account ------------------------------------------------------------------------

func TestDeleteRemovesTheAccount(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)

	require.NoError(t, f.account.Delete(f.ctx, s.User.ID, "correct-horse"))
	assert.NotContains(t, f.users.byID, s.User.ID)
	assert.False(t, f.loginWorks("ana@example.com", "correct-horse", device))
}

func TestDeleteNeedsTheRightPassword(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)

	requireKind(t, f.account.Delete(f.ctx, s.User.ID, "wrong-password"), apperr.Forbidden)
	requireKind(t, f.account.Delete(f.ctx, s.User.ID, ""), apperr.Forbidden, "a missing password is the same as a wrong one")
	assert.Contains(t, f.users.byID, s.User.ID, "nothing was deleted")
}

func TestDeleteWithoutAPasswordForSocialAccounts(t *testing.T) {
	f := newFlows(t)
	social := User{ID: uuid.New(), Email: "social@example.com"}
	f.users.byID[social.ID] = social
	require.NoError(t, f.account.Delete(f.ctx, social.ID, ""))
	assert.NotContains(t, f.users.byID, social.ID)
}

func TestDeleteErrors(t *testing.T) {
	boom := errors.New("db down")
	f := newFlows(t)
	requireKind(t, f.account.Delete(f.ctx, uuid.New(), "x"), apperr.Unauthenticated)

	s := f.register(t, "ana@example.com", device)
	f.users.fail["GetByID"] = boom
	requireKind(t, f.account.Delete(f.ctx, s.User.ID, "correct-horse"), apperr.Internal)
	delete(f.users.fail, "GetByID")

	f.account.Hasher = errHasher{&fakeHasher{}}
	requireKind(t, f.account.Delete(f.ctx, s.User.ID, "correct-horse"), apperr.Internal)
	f.account.Hasher = f.hasher

	f.users.fail["Delete"] = boom
	requireKind(t, f.account.Delete(f.ctx, s.User.ID, "correct-horse"), apperr.Internal)
}

func TestDeleteIsFineIfTheAccountIsAlreadyGone(t *testing.T) {
	f := newFlows(t)
	s := f.register(t, "ana@example.com", device)
	f.users.fail["Delete"] = ErrNotFound // removed by a concurrent request between the lookup and the delete
	assert.NoError(t, f.account.Delete(f.ctx, s.User.ID, "correct-horse"))
}
