package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

var (
	errNoProviderEmail = apperr.New(apperr.Unauthenticated, "the sign-in provider did not share an email address")
	errEmailInUse      = apperr.New(apperr.Conflict, "an account with this email already exists: sign in with your password")
)

type SocialInput struct {
	Provider    Provider
	Token       string // the provider's ID token
	Nonce       string // raw nonce (Apple)
	DisplayName string // from the client; Apple gives the name only on the first authorization
	DeviceID    uuid.UUID
}

type SocialDeps struct {
	Users    UserRepository
	OAuth    OAuthRepository
	Sessions RefreshTokenRepository
	Verifier IdentityVerifier
	Auth     *AuthService // issues the session
	// Verifier of new accounts' unverified emails. Optional: nil sends nothing.
	Verification VerificationSender
	Now          func() time.Time
	Log          *zap.Logger
}

// SocialAuth signs people in with Apple or Google. See docs/features/auth.md for the account rules.
type SocialAuth struct{ SocialDeps }

func NewSocialAuth(d SocialDeps) *SocialAuth { return &SocialAuth{d} }

func (s *SocialAuth) SignIn(ctx context.Context, in SocialInput) (Session, error) {
	if in.DeviceID == uuid.Nil {
		return Session{}, invalid("deviceId is required")
	}
	id, err := s.Verifier.Verify(ctx, in.Provider, in.Token, in.Nonce)
	switch {
	case errors.Is(err, ErrProviderDisabled):
		return Session{}, apperr.New(apperr.Unavailable, string(in.Provider)+" sign-in is not configured")
	case errors.Is(err, ErrInvalidIdentityToken):
		return Session{}, apperr.New(apperr.Unauthenticated, "invalid sign-in token")
	case errors.Is(err, ErrVerifierUnavailable):
		s.Log.Error("cannot verify sign-in token", zap.String("provider", string(in.Provider)), zap.Error(err))
		return Session{}, apperr.New(apperr.Unavailable, "cannot verify sign-in tokens right now, try again shortly")
	case err != nil:
		return Session{}, fmt.Errorf("verify identity token: %w", err)
	}

	user, err := s.resolve(ctx, id, in.DisplayName)
	if err != nil {
		return Session{}, err
	}
	return s.Auth.startSession(ctx, user, in.DeviceID, longRefreshLifetime)
}

// resolve finds or creates the user for an identity. A uniqueness conflict means a concurrent first
// sign-in got there first, so it re-reads once; the second pass then finds what the other request made.
func (s *SocialAuth) resolve(ctx context.Context, id Identity, clientName string) (User, error) {
	var user User
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		user, err = s.resolveOnce(ctx, id, clientName)
		if !errors.Is(err, ErrEmailTaken) && !errors.Is(err, ErrIdentityTaken) {
			break
		}
	}
	if errors.Is(err, ErrEmailTaken) || errors.Is(err, ErrIdentityTaken) {
		return User{}, fmt.Errorf("resolve identity after retry: %w", err)
	}
	return user, err
}

func (s *SocialAuth) resolveOnce(ctx context.Context, id Identity, clientName string) (User, error) {
	userID, err := s.OAuth.FindUser(ctx, id.Provider, id.Subject)
	if err == nil { // a known identity signs in its user, whatever the provider says about the email now
		u, err := s.Users.GetByID(ctx, userID)
		if err != nil {
			return User{}, fmt.Errorf("find linked user: %w", err)
		}
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("find identity: %w", err)
	}

	email, err := normalizeEmail(id.Email)
	if err != nil {
		return User{}, errNoProviderEmail
	}
	existing, err := s.Users.GetByEmail(ctx, email)
	switch {
	case err == nil && id.EmailVerified:
		return s.linkExisting(ctx, existing, id)
	case err == nil:
		return User{}, errEmailInUse // an unverified provider email never links
	case !errors.Is(err, ErrNotFound):
		return User{}, fmt.Errorf("find user by email: %w", err)
	}
	return s.createUser(ctx, id, email, clientName)
}

// linkExisting ties a provider identity to an existing account whose email the provider has verified.
func (s *SocialAuth) linkExisting(ctx context.Context, u User, id Identity) (User, error) {
	now := s.Now()
	if u.EmailVerifiedAt == nil {
		// Nobody ever proved they own this address, so whoever registered it may know its password. The
		// provider has just proved the real owner is signing in: take the account away from them first.
		if err := s.Users.ClearPassword(ctx, u.ID, now); err != nil {
			return User{}, fmt.Errorf("clear password: %w", err)
		}
		if err := s.Sessions.RevokeAllForUser(ctx, u.ID, now); err != nil {
			return User{}, fmt.Errorf("revoke sessions: %w", err)
		}
		s.Log.Warn("linked a provider to an unverified account: its password and sessions were removed", zap.String("user_id", u.ID.String()))
	}
	if err := s.OAuth.Link(ctx, u.ID, id, now); err != nil {
		return User{}, err
	}
	if u.EmailVerifiedAt == nil {
		if err := s.Users.MarkEmailVerified(ctx, u.ID, now); err != nil {
			return User{}, fmt.Errorf("mark email verified: %w", err)
		}
	}
	linked, err := s.Users.GetByID(ctx, u.ID)
	if err != nil {
		return User{}, fmt.Errorf("reload user: %w", err)
	}
	return linked, nil
}

func (s *SocialAuth) createUser(ctx context.Context, id Identity, email, clientName string) (User, error) {
	now := s.Now()
	n := NewUser{Email: email, DisplayName: providerDisplayName(email, id.Name, clientName), Timezone: defaultTimezone}
	if id.EmailVerified {
		n.EmailVerifiedAt = &now
	}
	u, err := s.Users.Create(ctx, n)
	if err != nil {
		return User{}, err
	}
	if err := s.OAuth.Link(ctx, u.ID, id, now); err != nil {
		return User{}, err
	}
	s.Log.Info("user created through a provider", zap.String("user_id", u.ID.String()), zap.String("provider", string(id.Provider)))
	if !id.EmailVerified && s.Verification != nil {
		if err := s.Verification.Send(ctx, u); err != nil {
			s.Log.Error("could not send verification email", zap.String("user_id", u.ID.String()), zap.Error(err))
		}
	}
	return u, nil
}

// providerDisplayName picks the first usable name (the provider's, then the client's, then the email's
// local part), trimmed and cut to the allowed length rather than rejected: it is not user input to refuse.
func providerDisplayName(email string, candidates ...string) string {
	for _, c := range candidates {
		if name := strings.TrimSpace(c); name != "" {
			if utf8.RuneCountInString(name) > maxDisplayLen {
				name = string([]rune(name)[:maxDisplayLen])
			}
			return name
		}
	}
	return defaultDisplayName(email)
}
