package service

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// clock is a controllable time source.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

var epoch = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// failures lets a test make a named fake method return an error.
type failures map[string]error

// --- users ---------------------------------------------------------------------------------

type fakeUsers struct {
	byID map[uuid.UUID]User
	fail failures
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byID: map[uuid.UUID]User{}, fail: failures{}} }

func (f *fakeUsers) Create(_ context.Context, email string, hash *string, name, tz string) (User, error) {
	if err := f.fail["Create"]; err != nil {
		return User{}, err
	}
	for _, u := range f.byID {
		if strings.EqualFold(u.Email, email) {
			return User{}, ErrEmailTaken
		}
	}
	u := User{ID: uuid.New(), Email: email, PasswordHash: hash, DisplayName: name, Timezone: tz}
	f.byID[u.ID] = u
	return u, nil
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (User, error) {
	if err := f.fail["GetByEmail"]; err != nil {
		return User{}, err
	}
	for _, u := range f.byID {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (f *fakeUsers) GetByID(_ context.Context, id uuid.UUID) (User, error) {
	if err := f.fail["GetByID"]; err != nil {
		return User{}, err
	}
	u, ok := f.byID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) UpdateProfile(_ context.Context, id uuid.UUID, name, tz *string, _ time.Time) (User, error) {
	if err := f.fail["UpdateProfile"]; err != nil {
		return User{}, err
	}
	u, ok := f.byID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if name != nil {
		u.DisplayName = *name
	}
	if tz != nil {
		u.Timezone = *tz
	}
	f.byID[id] = u
	return u, nil
}

// --- refresh tokens ------------------------------------------------------------------------

// fakeTokens mirrors the Postgres repository's semantics: one live token per user+device,
// and Rotate is all-or-nothing.
type fakeTokens struct {
	byID map[uuid.UUID]*RefreshToken
	fail failures
	// beforeRotate runs at the start of Rotate, to simulate another request winning a race.
	beforeRotate func()
}

func newFakeTokens() *fakeTokens {
	return &fakeTokens{byID: map[uuid.UUID]*RefreshToken{}, fail: failures{}}
}

func (f *fakeTokens) insert(t NewToken) *RefreshToken {
	rt := &RefreshToken{ID: uuid.New(), UserID: t.UserID, DeviceID: t.DeviceID, TokenHash: t.TokenHash,
		LifetimeSeconds: t.LifetimeSeconds, ExpiresAt: t.ExpiresAt}
	f.byID[rt.ID] = rt
	return rt
}

func (f *fakeTokens) Issue(_ context.Context, t NewToken, now time.Time) error {
	if err := f.fail["Issue"]; err != nil {
		return err
	}
	for _, rt := range f.byID {
		if rt.UserID == t.UserID && rt.DeviceID == t.DeviceID && rt.RevokedAt == nil {
			rt.RevokedAt = &now
		}
	}
	f.insert(t)
	return nil
}

func (f *fakeTokens) GetByHash(_ context.Context, hash string) (RefreshToken, error) {
	if err := f.fail["GetByHash"]; err != nil {
		return RefreshToken{}, err
	}
	for _, rt := range f.byID {
		if rt.TokenHash == hash {
			return *rt, nil
		}
	}
	return RefreshToken{}, ErrNotFound
}

func (f *fakeTokens) GetByID(_ context.Context, id uuid.UUID) (RefreshToken, error) {
	if err := f.fail["GetByID"]; err != nil {
		return RefreshToken{}, err
	}
	rt, ok := f.byID[id]
	if !ok {
		return RefreshToken{}, ErrNotFound
	}
	return *rt, nil
}

func (f *fakeTokens) Rotate(_ context.Context, oldID uuid.UUID, next NewToken, now time.Time) (bool, error) {
	if f.beforeRotate != nil {
		hook := f.beforeRotate
		f.beforeRotate = nil
		hook()
	}
	if err := f.fail["Rotate"]; err != nil {
		return false, err
	}
	old, ok := f.byID[oldID]
	if !ok || old.RevokedAt != nil {
		return false, nil
	}
	old.RevokedAt = &now
	n := f.insert(next)
	old.ReplacedBy = &n.ID
	return true, nil
}

func (f *fakeTokens) Revoke(_ context.Context, id uuid.UUID, now time.Time) error {
	if err := f.fail["Revoke"]; err != nil {
		return err
	}
	if rt, ok := f.byID[id]; ok && rt.RevokedAt == nil {
		rt.RevokedAt = &now
	}
	return nil
}

func (f *fakeTokens) RevokeAllForUser(_ context.Context, userID uuid.UUID, now time.Time) error {
	if err := f.fail["RevokeAllForUser"]; err != nil {
		return err
	}
	for _, rt := range f.byID {
		if rt.UserID == userID && rt.RevokedAt == nil {
			rt.RevokedAt = &now
		}
	}
	return nil
}

// live returns the user's non-revoked tokens.
func (f *fakeTokens) live(userID uuid.UUID) []*RefreshToken {
	var out []*RefreshToken
	for _, rt := range f.byID {
		if rt.UserID == userID && rt.RevokedAt == nil {
			out = append(out, rt)
		}
	}
	return out
}

// --- hasher and access tokens --------------------------------------------------------------

// fakeHasher is deterministic and cheap, and counts Dummy calls so tests can assert the
// timing-equalisation path ran.
type fakeHasher struct{ dummyCalls int }

func (h *fakeHasher) Hash(pw string) (string, error)      { return "hash:" + pw, nil }
func (h *fakeHasher) Verify(pw, phc string) (bool, error) { return phc == "hash:"+pw, nil }
func (h *fakeHasher) Dummy(string)                        { h.dummyCalls++ }

type fakeAccess struct{ fail error }

func (a fakeAccess) Issue(id uuid.UUID) (string, int, error) {
	if a.fail != nil {
		return "", 0, a.fail
	}
	return "access-for-" + id.String(), 900, nil
}

// --- users: slice 2 additions --------------------------------------------------------------

func (f *fakeUsers) SetPassword(_ context.Context, id uuid.UUID, hash string, _ time.Time) error {
	if err := f.fail["SetPassword"]; err != nil {
		return err
	}
	u, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = &hash
	f.byID[id] = u
	return nil
}

func (f *fakeUsers) MarkEmailVerified(_ context.Context, id uuid.UUID, now time.Time) error {
	if err := f.fail["MarkEmailVerified"]; err != nil {
		return err
	}
	u, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	if u.EmailVerifiedAt == nil { // idempotent: the first verification time is kept
		u.EmailVerifiedAt = &now
	}
	f.byID[id] = u
	return nil
}

func (f *fakeUsers) Delete(_ context.Context, id uuid.UUID) error {
	if err := f.fail["Delete"]; err != nil {
		return err
	}
	if _, ok := f.byID[id]; !ok {
		return ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

// --- email tokens --------------------------------------------------------------------------

type emailTokenRow struct {
	NewEmailToken
	createdAt  time.Time
	consumedAt *time.Time
}

// fakeEmailTokens mirrors the Postgres repository: a new token supersedes older unused ones of the same
// purpose, and Consume is single-use and honours expiry and purpose.
type fakeEmailTokens struct {
	rows []*emailTokenRow
	fail failures
}

func newFakeEmailTokens() *fakeEmailTokens { return &fakeEmailTokens{fail: failures{}} }

func (f *fakeEmailTokens) Replace(_ context.Context, t NewEmailToken, now time.Time) error {
	if err := f.fail["Replace"]; err != nil {
		return err
	}
	for _, r := range f.rows {
		if r.UserID == t.UserID && r.Purpose == t.Purpose && r.consumedAt == nil {
			r.consumedAt = &now
		}
	}
	f.rows = append(f.rows, &emailTokenRow{NewEmailToken: t, createdAt: now})
	return nil
}

func (f *fakeEmailTokens) LastCreated(_ context.Context, userID uuid.UUID, purpose EmailPurpose) (time.Time, error) {
	if err := f.fail["LastCreated"]; err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, r := range f.rows {
		if r.UserID == userID && r.Purpose == purpose && r.createdAt.After(latest) {
			latest = r.createdAt
		}
	}
	if latest.IsZero() {
		return time.Time{}, ErrNotFound
	}
	return latest, nil
}

func (f *fakeEmailTokens) Consume(_ context.Context, hash string, purpose EmailPurpose, now time.Time) (uuid.UUID, error) {
	if err := f.fail["Consume"]; err != nil {
		return uuid.Nil, err
	}
	for _, r := range f.rows {
		if r.TokenHash == hash && r.Purpose == purpose && r.consumedAt == nil && now.Before(r.ExpiresAt) {
			r.consumedAt = &now
			return r.UserID, nil
		}
	}
	return uuid.Nil, ErrNotFound
}

// --- mailer --------------------------------------------------------------------------------

type sentMail struct {
	kind, to, name, link string
}

// token is the value of the link's ?token= parameter, as the user would click it.
func (m sentMail) token() string {
	u, _ := url.Parse(m.link)
	return u.Query().Get("token")
}

type fakeMailer struct {
	sent []sentMail
	err  error
}

func (m *fakeMailer) SendVerification(_ context.Context, to, name, link string) error {
	m.sent = append(m.sent, sentMail{"verify", to, name, link})
	return m.err
}

func (m *fakeMailer) SendPasswordReset(_ context.Context, to, name, link string) error {
	m.sent = append(m.sent, sentMail{"reset", to, name, link})
	return m.err
}

func (m *fakeMailer) last() sentMail { return m.sent[len(m.sent)-1] }
