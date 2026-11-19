package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

type OAuthAccounts struct{ q *sqlc.Queries }

func NewOAuthAccounts(db sqlc.DBTX) *OAuthAccounts { return &OAuthAccounts{q: sqlc.New(db)} }

func (r *OAuthAccounts) FindUser(ctx context.Context, p service.Provider, subject string) (uuid.UUID, error) {
	id, err := r.q.GetOAuthUserID(ctx, sqlc.GetOAuthUserIDParams{Provider: string(p), Subject: subject})
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	return id, nil
}

// Link ties an identity to a user. The (provider, subject) pair is unique, so linking it twice, even to
// the same user, is service.ErrIdentityTaken.
func (r *OAuthAccounts) Link(ctx context.Context, userID uuid.UUID, id service.Identity, now time.Time) error {
	var email *string
	if id.Email != "" {
		email = &id.Email
	}
	err := r.q.InsertOAuthAccount(ctx, sqlc.InsertOAuthAccountParams{
		UserID: userID, Provider: string(id.Provider), Subject: id.Subject, Email: text(email), CreatedAt: now,
	})
	if isUniqueViolation(err, "oauth_account_provider_subject_key") {
		return service.ErrIdentityTaken
	}
	return err
}
