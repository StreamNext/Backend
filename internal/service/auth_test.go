package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/auth"
)

type env struct {
	svc    *AuthService
	users  *fakeUsers
	tokens *fakeTokens
	hasher *fakeHasher
	clock  *clock
	ctx    context.Context
}

func newEnv(grace time.Duration) *env {
	e := &env{users: newFakeUsers(), tokens: newFakeTokens(), hasher: &fakeHasher{}, clock: &clock{t: epoch}, ctx: context.Background()}
	e.svc = NewAuthService(AuthDeps{Users: e.users, Tokens: e.tokens, Hasher: e.hasher, Access: fakeAccess{},
		Grace: grace, Now: e.clock.Now, Log: zap.NewNop()})
	return e
}

var device = uuid.MustParse("11111111-1111-4111-8111-111111111111")
var otherDevice = uuid.MustParse("22222222-2222-4222-8222-222222222222")

func (e *env) register(t *testing.T, email string, dev uuid.UUID) Session {
	t.Helper()
	s, err := e.svc.Register(e.ctx, RegisterInput{Email: email, Password: "correct-horse", DeviceID: dev})
	require.NoError(t, err)
	return s
}

func requireKind(t *testing.T, err error, kind apperr.Kind) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, kind, apperr.From(err).Kind, err.Error())
}

// --- Register ------------------------------------------------------------------------------

func TestRegisterCreatesUserAndSession(t *testing.T) {
	e := newEnv(10 * time.Second)
	s, err := e.svc.Register(e.ctx, RegisterInput{Email: "  Ana@Example.COM ", Password: "correct-horse", DeviceID: device})
	require.NoError(t, err)

	assert.Equal(t, "ana@example.com", s.User.Email, "email is trimmed and lowercased")
	assert.Equal(t, "ana", s.User.DisplayName, "display name defaults to the local part")
	assert.Equal(t, "UTC", s.User.Timezone)
	assert.Equal(t, 900, s.ExpiresIn)
	assert.Equal(t, "access-for-"+s.User.ID.String(), s.AccessToken)
	require.NotNil(t, s.User.PasswordHash)
	assert.Equal(t, "hash:correct-horse", *s.User.PasswordHash, "stored as a hash from the hasher")

	stored, err := e.tokens.GetByHash(e.ctx, auth.HashToken(s.RefreshToken))
	require.NoError(t, err, "the refresh token is stored by its hash")
	assert.Equal(t, device, stored.DeviceID)
	assert.Equal(t, epoch.Add(24*time.Hour), stored.ExpiresAt, "default lifetime is 24h")
}

func TestRegisterRememberMeLastsNinetyDays(t *testing.T) {
	e := newEnv(10 * time.Second)
	s, err := e.svc.Register(e.ctx, RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device, RememberMe: true})
	require.NoError(t, err)
	stored, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(s.RefreshToken))
	assert.Equal(t, epoch.Add(90*24*time.Hour), stored.ExpiresAt)
}

func TestRegisterUsesGivenNameAndTimezone(t *testing.T) {
	e := newEnv(10 * time.Second)
	s, err := e.svc.Register(e.ctx, RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device,
		DisplayName: "  Ana María ", Timezone: "America/Bogota"})
	require.NoError(t, err)
	assert.Equal(t, "Ana María", s.User.DisplayName)
	assert.Equal(t, "America/Bogota", s.User.Timezone)
}

func TestRegisterValidation(t *testing.T) {
	e := newEnv(10 * time.Second)
	good := RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device}
	cases := map[string]func(*RegisterInput){
		"empty email":       func(r *RegisterInput) { r.Email = "" },
		"no at sign":        func(r *RegisterInput) { r.Email = "nope" },
		"no domain dot":     func(r *RegisterInput) { r.Email = "a@localhost" },
		"display form":      func(r *RegisterInput) { r.Email = "Ana <a@b.co>" },
		"short password":    func(r *RegisterInput) { r.Password = "123456789" },
		"huge password":     func(r *RegisterInput) { r.Password = string(make([]byte, 129)) },
		"missing device":    func(r *RegisterInput) { r.DeviceID = uuid.Nil },
		"name too long":     func(r *RegisterInput) { r.DisplayName = string(make([]rune, 41)) + "x" },
		"blank name":        func(r *RegisterInput) { r.DisplayName = "   " },
		"unknown timezone":  func(r *RegisterInput) { r.Timezone = "Mars/Olympus" },
		"server-local zone": func(r *RegisterInput) { r.Timezone = "Local" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := good
			mutate(&in)
			_, err := e.svc.Register(e.ctx, in)
			requireKind(t, err, apperr.InvalidArgument)
		})
	}
	assert.Empty(t, e.users.byID, "nothing is created for invalid input")
}

func TestRegisterDuplicateEmailIsConflict(t *testing.T) {
	e := newEnv(10 * time.Second)
	e.register(t, "ana@example.com", device)
	_, err := e.svc.Register(e.ctx, RegisterInput{Email: "ANA@example.com", Password: "another-pass-1", DeviceID: otherDevice})
	requireKind(t, err, apperr.Conflict)
}

func TestRegisterInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	for name, setup := range map[string]func(*env){
		"create user":  func(e *env) { e.users.fail["Create"] = boom },
		"issue token":  func(e *env) { e.tokens.fail["Issue"] = boom },
		"access token": func(e *env) { e.svc.Access = fakeAccess{fail: boom} },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(10 * time.Second)
			setup(e)
			_, err := e.svc.Register(e.ctx, RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device})
			requireKind(t, err, apperr.Internal)
		})
	}
}

// --- Login ---------------------------------------------------------------------------------

func TestLoginSucceeds(t *testing.T) {
	e := newEnv(10 * time.Second)
	reg := e.register(t, "ana@example.com", device)

	s, err := e.svc.Login(e.ctx, LoginInput{Email: "ANA@example.com", Password: "correct-horse", DeviceID: device})
	require.NoError(t, err)
	assert.Equal(t, reg.User.ID, s.User.ID)
	assert.NotEqual(t, reg.RefreshToken, s.RefreshToken)
	assert.Zero(t, e.hasher.dummyCalls)
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(10 * time.Second)
	e.register(t, "ana@example.com", device)
	social := User{ID: uuid.New(), Email: "social@example.com"} // no password hash
	e.users.byID[social.ID] = social

	wrongPassword, err1 := e.svc.Login(e.ctx, LoginInput{Email: "ana@example.com", Password: "wrong-password", DeviceID: device})
	assert.Equal(t, 0, e.hasher.dummyCalls, "a real account is verified for real")

	_, err2 := e.svc.Login(e.ctx, LoginInput{Email: "nobody@example.com", Password: "wrong-password", DeviceID: device})
	_, err3 := e.svc.Login(e.ctx, LoginInput{Email: "social@example.com", Password: "wrong-password", DeviceID: device})
	_, err4 := e.svc.Login(e.ctx, LoginInput{Email: "not an email", Password: "wrong-password", DeviceID: device})

	assert.Zero(t, wrongPassword)
	for _, err := range []error{err1, err2, err3, err4} {
		requireKind(t, err, apperr.Unauthenticated)
		assert.Equal(t, "invalid credentials", err.Error(), "same message whatever the reason")
	}
	assert.Equal(t, 3, e.hasher.dummyCalls, "unknown email, social-only and malformed email all do the dummy hash")
}

func TestLoginRememberMeAndDeviceRequired(t *testing.T) {
	e := newEnv(10 * time.Second)
	e.register(t, "ana@example.com", device)
	s, err := e.svc.Login(e.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: device, RememberMe: true})
	require.NoError(t, err)
	stored, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(s.RefreshToken))
	assert.Equal(t, epoch.Add(90*24*time.Hour), stored.ExpiresAt)

	_, err = e.svc.Login(e.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse"})
	requireKind(t, err, apperr.InvalidArgument)
}

func TestLoginInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	e := newEnv(10 * time.Second)
	e.register(t, "ana@example.com", device)
	in := LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: device}

	e.users.fail["GetByEmail"] = boom
	_, err := e.svc.Login(e.ctx, in)
	requireKind(t, err, apperr.Internal)
	delete(e.users.fail, "GetByEmail")

	// A stored hash the hasher can't read is a server fault, not a wrong password.
	for _, u := range e.users.byID {
		bad := "garbage"
		u.PasswordHash = &bad
		e.users.byID[u.ID] = u
	}
	e.svc.Hasher = errHasher{&fakeHasher{}}
	_, err = e.svc.Login(e.ctx, in)
	requireKind(t, err, apperr.Internal)
}

type errHasher struct{ *fakeHasher }

func (errHasher) Verify(string, string) (bool, error) { return false, errors.New("corrupt hash") }

func TestRegisterHashError(t *testing.T) {
	e := newEnv(10 * time.Second)
	e.svc.Hasher = hashFails{&fakeHasher{}}
	_, err := e.svc.Register(e.ctx, RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device})
	requireKind(t, err, apperr.Internal)
}

type hashFails struct{ *fakeHasher }

func (hashFails) Hash(string) (string, error) { return "", errors.New("no entropy") }

func TestLoginOnSameDeviceRevokesPreviousRefreshToken(t *testing.T) {
	e := newEnv(10 * time.Second)
	reg := e.register(t, "ana@example.com", device)
	_, err := e.svc.Login(e.ctx, LoginInput{Email: "ana@example.com", Password: "correct-horse", DeviceID: device})
	require.NoError(t, err)

	assert.Len(t, e.tokens.live(reg.User.ID), 1, "one live token per device")
	old, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(reg.RefreshToken))
	assert.NotNil(t, old.RevokedAt)
}

// --- Refresh -------------------------------------------------------------------------------

func TestRefreshRotatesAndSlidesTheWindow(t *testing.T) {
	e := newEnv(10 * time.Second)
	s, err := e.svc.Register(e.ctx, RegisterInput{Email: "a@b.co", Password: "correct-horse", DeviceID: device, RememberMe: true})
	require.NoError(t, err)

	e.clock.Advance(time.Hour)
	next, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err)
	assert.NotEqual(t, s.RefreshToken, next.RefreshToken)
	assert.Equal(t, s.User.ID, next.User.ID)

	old, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(s.RefreshToken))
	assert.NotNil(t, old.RevokedAt, "the presented token is revoked")
	assert.NotNil(t, old.ReplacedBy)
	fresh, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(next.RefreshToken))
	assert.Equal(t, epoch.Add(time.Hour+90*24*time.Hour), fresh.ExpiresAt, "remember-me lifetime carries forward")
	assert.Len(t, e.tokens.live(s.User.ID), 1)
}

func TestRefreshRejectsUnknownWrongDeviceAndExpired(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	e.register(t, "c@d.co", otherDevice) // a second user with a live session

	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: "not-a-token", DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)

	_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: otherDevice})
	requireKind(t, err, apperr.Unauthenticated)
	assert.Len(t, e.tokens.live(s.User.ID), 1, "a wrong-device attempt has no side effects")

	e.clock.Advance(24*time.Hour + time.Second)
	_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
	assert.Len(t, e.tokens.live(s.User.ID), 1, "an expired token is rejected without revoking anything")
}

func TestRefreshReuseRevokesEverySession(t *testing.T) {
	e := newEnv(10 * time.Second)
	phone := e.register(t, "a@b.co", device)
	// Same user on a second device.
	tablet, err := e.svc.Login(e.ctx, LoginInput{Email: "a@b.co", Password: "correct-horse", DeviceID: otherDevice})
	require.NoError(t, err)

	rotated, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: phone.RefreshToken, DeviceID: device})
	require.NoError(t, err)

	e.clock.Advance(11 * time.Second) // outside the grace window
	_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: phone.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
	assert.Empty(t, e.tokens.live(phone.User.ID), "every token of the user is revoked, on every device")

	for _, tok := range []string{rotated.RefreshToken, tablet.RefreshToken} {
		_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: tok, DeviceID: device})
		requireKind(t, err, apperr.Unauthenticated)
	}
}

func TestRefreshGraceWindowRecoversALostResponse(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	first, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err) // ...but the client never received this response

	e.clock.Advance(5 * time.Second)
	retry, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err, "retrying the old token inside the window issues a fresh pair")
	assert.NotEqual(t, first.RefreshToken, retry.RefreshToken)

	assert.Len(t, e.tokens.live(s.User.ID), 1, "the unused successor was replaced, so only one token is live")
	lost, _ := e.tokens.GetByHash(e.ctx, auth.HashToken(first.RefreshToken))
	assert.NotNil(t, lost.RevokedAt)
}

func TestRefreshGraceDoesNotApplyOnceTheSuccessorWasUsed(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	first, _ := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: first.RefreshToken, DeviceID: device}) // successor used
	require.NoError(t, err)

	e.clock.Advance(2 * time.Second) // still inside the window for the original token
	_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
	assert.Empty(t, e.tokens.live(s.User.ID), "treated as reuse")
}

func TestRefreshGraceDisabledMeansImmediateReuseDetection(t *testing.T) {
	e := newEnv(0)
	s := e.register(t, "a@b.co", device)
	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err)
	_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
	assert.Empty(t, e.tokens.live(s.User.ID))
}

func TestRefreshConcurrentDuplicateEndsWithOneLiveToken(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	// Another request rotates the same token between our read and our Rotate call.
	e.tokens.beforeRotate = func() {
		_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		require.NoError(t, err)
	}
	got, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	require.NoError(t, err, "the loser of the race is served through the grace path, not logged out")
	assert.NotEmpty(t, got.RefreshToken)
	assert.Len(t, e.tokens.live(s.User.ID), 1)
}

func TestRefreshWhenTheUserNoLongerExists(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	delete(e.users.byID, s.User.ID)
	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
}

func TestRefreshInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	steps := map[string]func(*env, *fakeTokens, *fakeUsers){
		"lookup":       func(_ *env, t *fakeTokens, _ *fakeUsers) { t.fail["GetByHash"] = boom },
		"find user":    func(_ *env, _ *fakeTokens, u *fakeUsers) { u.fail["GetByID"] = boom },
		"rotate":       func(_ *env, t *fakeTokens, _ *fakeUsers) { t.fail["Rotate"] = boom },
		"access token": func(e *env, _ *fakeTokens, _ *fakeUsers) { e.svc.Access = fakeAccess{fail: boom} },
	}
	for name, setup := range steps {
		t.Run(name, func(t *testing.T) {
			e := newEnv(10 * time.Second)
			s := e.register(t, "a@b.co", device)
			setup(e, e.tokens, e.users)
			_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
			requireKind(t, err, apperr.Internal)
		})
	}
}

func TestRefreshRevokedPathInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	t.Run("revoke all fails", func(t *testing.T) {
		e := newEnv(10 * time.Second)
		s := e.register(t, "a@b.co", device)
		_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		require.NoError(t, err)
		e.clock.Advance(time.Minute) // outside the grace window, so reuse handling runs
		e.tokens.fail["RevokeAllForUser"] = boom
		_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		requireKind(t, err, apperr.Internal)
	})
	t.Run("finding the successor fails", func(t *testing.T) {
		e := newEnv(10 * time.Second)
		s := e.register(t, "a@b.co", device)
		_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		require.NoError(t, err)
		e.tokens.fail["GetByID"] = boom // inside the grace window the successor is looked up
		_, err = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		requireKind(t, err, apperr.Internal)
	})

	// Losing a race and then failing to reload the token.
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)
	e.tokens.beforeRotate = func() {
		_, _ = e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
		e.tokens.fail["GetByID"] = boom
	}
	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Internal)
}

// --- Logout --------------------------------------------------------------------------------

func TestLogoutRevokesAndIsIdempotent(t *testing.T) {
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)

	require.NoError(t, e.svc.Logout(e.ctx, s.RefreshToken))
	assert.Empty(t, e.tokens.live(s.User.ID))
	require.NoError(t, e.svc.Logout(e.ctx, s.RefreshToken), "second logout is fine")
	require.NoError(t, e.svc.Logout(e.ctx, "never-issued"), "unknown token is fine")

	_, err := e.svc.Refresh(e.ctx, RefreshInput{RefreshToken: s.RefreshToken, DeviceID: device})
	requireKind(t, err, apperr.Unauthenticated)
}

func TestLogoutInfrastructureErrors(t *testing.T) {
	boom := errors.New("db down")
	e := newEnv(10 * time.Second)
	s := e.register(t, "a@b.co", device)

	e.tokens.fail["GetByHash"] = boom
	requireKind(t, e.svc.Logout(e.ctx, s.RefreshToken), apperr.Internal)
	delete(e.tokens.fail, "GetByHash")
	e.tokens.fail["Revoke"] = boom
	requireKind(t, e.svc.Logout(e.ctx, s.RefreshToken), apperr.Internal)
}
