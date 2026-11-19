package rest

import (
	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

func toAPIUser(u service.User) api.User {
	return api.User{Id: u.ID, Email: u.Email, EmailVerified: u.EmailVerifiedAt != nil, DisplayName: u.DisplayName, Timezone: u.Timezone}
}

func toAPISession(s service.Session) api.Session {
	return api.Session{AccessToken: s.AccessToken, RefreshToken: s.RefreshToken, ExpiresIn: s.ExpiresIn, User: toAPIUser(s.User)}
}
