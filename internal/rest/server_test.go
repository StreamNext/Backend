package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// --- stubs for the service layer -----------------------------------------------------------

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

// stubAuth returns a configured result and records the input it received.
type stubAuth struct {
	session  service.Session
	err      error
	register service.RegisterInput
	login    service.LoginInput
	refresh  service.RefreshInput
	logout   string
}

func (a *stubAuth) Register(_ context.Context, in service.RegisterInput) (service.Session, error) {
	a.register = in
	return a.session, a.err
}
func (a *stubAuth) Login(_ context.Context, in service.LoginInput) (service.Session, error) {
	a.login = in
	return a.session, a.err
}
func (a *stubAuth) Refresh(_ context.Context, in service.RefreshInput) (service.Session, error) {
	a.refresh = in
	return a.session, a.err
}
func (a *stubAuth) Logout(_ context.Context, token string) error {
	a.logout = token
	return a.err
}

type stubProfile struct {
	user      service.User
	err       error
	gotID     uuid.UUID
	gotUpdate service.UpdateProfileInput
}

func (p *stubProfile) Get(_ context.Context, id uuid.UUID) (service.User, error) {
	p.gotID = id
	return p.user, p.err
}
func (p *stubProfile) Update(_ context.Context, id uuid.UUID, in service.UpdateProfileInput) (service.User, error) {
	p.gotID, p.gotUpdate = id, in
	return p.user, p.err
}

type stubVerification struct {
	err                       error
	resendEmail, confirmToken string
}

func (v *stubVerification) Resend(_ context.Context, email string) error {
	v.resendEmail = email
	return v.err
}
func (v *stubVerification) Confirm(_ context.Context, token string) error {
	v.confirmToken = token
	return v.err
}

type stubReset struct {
	err                                     error
	requestEmail, resetToken, resetPassword string
}

func (r *stubReset) Request(_ context.Context, email string) error {
	r.requestEmail = email
	return r.err
}
func (r *stubReset) Reset(_ context.Context, token, newPassword string) error {
	r.resetToken, r.resetPassword = token, newPassword
	return r.err
}

type stubAccount struct {
	session        service.Session
	err            error
	gotUser        uuid.UUID
	gotChange      service.ChangePasswordInput
	deletePassword string
	deleteCalls    int
}

func (a *stubAccount) ChangePassword(_ context.Context, id uuid.UUID, in service.ChangePasswordInput) (service.Session, error) {
	a.gotUser, a.gotChange = id, in
	return a.session, a.err
}
func (a *stubAccount) Delete(_ context.Context, id uuid.UUID, password string) error {
	a.gotUser, a.deletePassword = id, password
	a.deleteCalls++
	return a.err
}

// stubTokens maps accepted bearer tokens to user ids.
type stubTokens map[string]uuid.UUID

func (s stubTokens) Parse(token string) (uuid.UUID, error) {
	if id, ok := s[token]; ok {
		return id, nil
	}
	return uuid.Nil, errors.New("bad token")
}

var (
	userA   = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	devID   = "11111111-1111-4111-8111-111111111111"
	fixture = service.User{ID: userA, Email: "ana@example.com", DisplayName: "Ana", Timezone: "UTC"}
)

type harness struct {
	srv          *Server
	handler      http.Handler
	auth         *stubAuth
	profile      *stubProfile
	verification *stubVerification
	reset        *stubReset
	account      *stubAccount
}

// newHarness builds a Server with stub services. mutate may adjust Deps before construction.
func newHarness(t *testing.T, mutate ...func(*Deps)) *harness {
	t.Helper()
	h := &harness{
		auth:         &stubAuth{session: service.Session{AccessToken: "acc", RefreshToken: "ref", ExpiresIn: 900, User: fixture}},
		profile:      &stubProfile{user: fixture},
		verification: &stubVerification{}, reset: &stubReset{},
		account: &stubAccount{session: service.Session{AccessToken: "acc2", RefreshToken: "ref2", ExpiresIn: 900, User: fixture}},
	}
	d := Deps{DB: fakeDB{}, Auth: h.auth, Verification: h.verification, Reset: h.reset, Account: h.account, Profile: h.profile,
		Tokens: stubTokens{"good-token": userA}, Log: zap.NewNop()}
	for _, m := range mutate {
		m(&d)
	}
	srv, err := NewServer(d)
	require.NoError(t, err)
	h.srv, h.handler = srv, srv.Handler()
	return h
}

// --- request helpers -----------------------------------------------------------------------

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, h, method, path, "", nil)
}

func send(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// requireError asserts the response is the contract's {code,message} error.
func requireError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var e api.Error
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &e))
	assert.Equal(t, code, e.Code)
	assert.NotEmpty(t, e.Message)
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }
