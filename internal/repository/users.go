package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

type Users struct{ q *sqlc.Queries }

func NewUsers(db sqlc.DBTX) *Users { return &Users{q: sqlc.New(db)} }

func (r *Users) Create(ctx context.Context, n service.NewUser) (service.User, error) {
	row, err := r.q.CreateUser(ctx, sqlc.CreateUserParams{
		Email: n.Email, PasswordHash: text(n.PasswordHash), EmailVerifiedAt: n.EmailVerifiedAt,
		DisplayName: n.DisplayName, Timezone: n.Timezone,
	})
	if isUniqueViolation(err, "users_email_key") {
		return service.User{}, service.ErrEmailTaken
	}
	if err != nil {
		return service.User{}, err
	}
	return toUser(row), nil
}

func (r *Users) GetByEmail(ctx context.Context, email string) (service.User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return service.User{}, notFound(err)
	}
	return toUser(row), nil
}

func (r *Users) GetByID(ctx context.Context, id uuid.UUID) (service.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		return service.User{}, notFound(err)
	}
	return toUser(row), nil
}

func (r *Users) UpdateProfile(ctx context.Context, id uuid.UUID, displayName, timezone, language *string, now time.Time) (service.User, error) {
	row, err := r.q.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		ID: id, DisplayName: text(displayName), Timezone: text(timezone), Language: text(language), Now: now,
	})
	if err != nil {
		return service.User{}, notFound(err)
	}
	return toUser(row), nil
}

func (r *Users) SetPassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error {
	return affected(r.q.SetUserPassword(ctx, sqlc.SetUserPasswordParams{ID: id, PasswordHash: passwordHash, Now: now}))
}

func (r *Users) ClearPassword(ctx context.Context, id uuid.UUID, now time.Time) error {
	return affected(r.q.ClearUserPassword(ctx, sqlc.ClearUserPasswordParams{ID: id, Now: now}))
}

func (r *Users) MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error {
	return affected(r.q.MarkUserEmailVerified(ctx, sqlc.MarkUserEmailVerifiedParams{ID: id, Now: now}))
}

func (r *Users) Delete(ctx context.Context, id uuid.UUID) error {
	return affected(r.q.DeleteUser(ctx, id))
}

func toUser(u sqlc.User) service.User {
	var hash *string
	if u.PasswordHash.Valid {
		hash = &u.PasswordHash.String
	}
	return service.User{
		ID: u.ID, Email: u.Email, PasswordHash: hash, EmailVerifiedAt: u.EmailVerifiedAt,
		DisplayName: u.DisplayName, Timezone: u.Timezone, Language: service.Language(u.Language),
	}
}
