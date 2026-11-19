package rest

import (
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

func toAPIUser(u service.User) api.User {
	return api.User{Id: u.ID, Email: u.Email, EmailVerified: u.EmailVerifiedAt != nil, DisplayName: u.DisplayName, Timezone: u.Timezone, Language: language(u.Language)}
}

func toAPISession(s service.Session) api.Session {
	return api.Session{AccessToken: s.AccessToken, RefreshToken: s.RefreshToken, ExpiresIn: s.ExpiresIn, User: toAPIUser(s.User)}
}

// language falls back to English for a user stored without one.
func language(l service.Language) api.Language {
	if l.Valid() {
		return api.Language(l)
	}
	return api.En
}

// The contract leaves out fields that are unknown: an empty text, a zero count, a missing date.
func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func optDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

func toAPISearchResults(r service.SearchResults) api.SeriesSearchResults {
	results := make([]api.SeriesSummary, 0, len(r.Results))
	for _, s := range r.Results {
		results = append(results, api.SeriesSummary{
			Id: s.ID, Name: s.Name, OriginalName: optString(s.OriginalName), Overview: optString(s.Overview),
			Year: optString(s.Year), Network: optString(s.Network), Status: api.SeriesStatus(s.Status), Image: optString(s.Image),
		})
	}
	return api.SeriesSearchResults{Results: results, Page: r.Page, HasMore: r.HasMore}
}

func toAPISeries(d service.SeriesDetail) api.SeriesDetail {
	genres := make([]api.Genre, 0, len(d.Genres))
	for _, g := range d.Genres {
		genres = append(genres, api.Genre{Id: g.ID, Name: g.Name})
	}
	seasons := make([]api.SeasonSummary, 0, len(d.Seasons))
	for _, s := range d.Seasons {
		seasons = append(seasons, api.SeasonSummary{Number: s.Number, Name: optString(s.Name), Image: optString(s.Image), EpisodeCount: s.EpisodeCount})
	}
	days := make([]api.SeriesDetailAirsDays, 0, len(d.AirsDays))
	for _, day := range d.AirsDays {
		days = append(days, api.SeriesDetailAirsDays(day))
	}
	return api.SeriesDetail{
		Id: d.ID, Slug: d.Slug, Name: d.Name, OriginalName: d.OriginalName, Overview: d.Overview, Image: optString(d.Image),
		Status: api.SeriesStatus(d.Status), FirstAired: optDate(d.FirstAired), LastAired: optDate(d.LastAired), NextAired: optDate(d.NextAired),
		AirsTime: optString(d.AirsTime), AirsDays: days, OriginalCountry: optString(d.OriginalCountry),
		OriginalLanguage: optString(d.OriginalLanguage), Network: optString(d.Network), AverageRuntime: optInt(d.AverageRuntime),
		Genres: genres, Seasons: seasons, SyncedAt: d.SyncedAt,
	}
}

func toAPIEpisodes(eps []service.Episode) api.EpisodeList {
	out := make([]api.Episode, 0, len(eps))
	for _, e := range eps {
		out = append(out, api.Episode{
			Id: e.ID, SeasonNumber: e.SeasonNumber, Number: e.Number, AbsoluteNumber: optInt(e.AbsoluteNumber), Name: e.Name,
			Overview: optString(e.Overview), Aired: optDate(e.Aired), Runtime: optInt(e.Runtime), Image: optString(e.Image),
			FinaleType: optString(e.FinaleType),
		})
	}
	return api.EpisodeList{Episodes: out}
}
