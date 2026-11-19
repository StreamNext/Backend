package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/StreamNext/Backend/internal/apperr"
)

type UpdateProfileInput struct {
	DisplayName *string
	Timezone    *string
}

// ProfileService reads and updates the signed-in user's own profile.
type ProfileService struct {
	users UserRepository
	now   func() time.Time
}

func NewProfileService(users UserRepository, now func() time.Time) *ProfileService {
	return &ProfileService{users: users, now: now}
}

func (p *ProfileService) Get(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := p.users.GetByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		// A valid token for a user that no longer exists (deleted account).
		return User{}, apperr.New(apperr.Unauthenticated, "invalid credentials")
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return u, nil
}

func (p *ProfileService) Update(ctx context.Context, id uuid.UUID, in UpdateProfileInput) (User, error) {
	if in.DisplayName == nil && in.Timezone == nil {
		return User{}, invalid("nothing to update")
	}
	if in.DisplayName != nil {
		name, err := normalizeDisplayName(*in.DisplayName)
		if err != nil {
			return User{}, err
		}
		in.DisplayName = &name
	}
	if in.Timezone != nil {
		tz, err := normalizeTimezone(*in.Timezone)
		if err != nil {
			return User{}, err
		}
		in.Timezone = &tz
	}
	u, err := p.users.UpdateProfile(ctx, id, in.DisplayName, in.Timezone, p.now())
	if errors.Is(err, ErrNotFound) {
		return User{}, apperr.New(apperr.Unauthenticated, "invalid credentials")
	}
	if err != nil {
		return User{}, fmt.Errorf("update profile: %w", err)
	}
	return u, nil
}
