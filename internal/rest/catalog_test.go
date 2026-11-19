package rest

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/service"
)

func aired(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestSearchSeries(t *testing.T) {
	h := newHarness(t)
	h.catalog.search = service.SearchResults{Page: 2, HasMore: true, Results: []service.SeriesSummary{
		{ID: 81189, Name: "Breaking Bad", OriginalName: "Breaking Bad", Overview: "About a teacher", Year: "2008", Network: "AMC", Status: service.StatusEnded, Image: "https://img/p.jpg"},
		{ID: 5, Name: "Bare", Status: service.StatusUnknown},
	}}
	rec := send(t, h.handler, http.MethodGet, "/v1/search/series?query=breaking%20bad&page=2", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, userA, h.catalog.gotUser, "the user comes from the token")
	assert.Equal(t, "breaking bad", h.catalog.gotQuery)
	assert.Equal(t, 2, h.catalog.gotPage)
	assert.JSONEq(t, `{"page":2,"hasMore":true,"results":[
	  {"id":81189,"name":"Breaking Bad","originalName":"Breaking Bad","overview":"About a teacher","year":"2008","network":"AMC","status":"ended","image":"https://img/p.jpg"},
	  {"id":5,"name":"Bare","status":"unknown"}]}`, rec.Body.String(), "unknown fields are left out, not sent empty")
}

func TestSearchSeriesWithoutAPageIsPageZeroForTheServiceToDefault(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodGet, "/v1/search/series?query=ab", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 0, h.catalog.gotPage)
	assert.JSONEq(t, `{"page":0,"hasMore":false,"results":[]}`, rec.Body.String(), "an empty result is an empty list, not null")
}

func TestSearchParametersAreChecked(t *testing.T) {
	h := newHarness(t)
	for name, path := range map[string]string{
		"no query":          "/v1/search/series",
		"page not a number": "/v1/search/series?query=ab&page=abc",
		"page twice":        "/v1/search/series?query=ab&page=1&page=2",
	} {
		t.Run(name, func(t *testing.T) {
			requireError(t, send(t, h.handler, http.MethodGet, path, "", bearer("good-token")), http.StatusBadRequest, "invalid_argument")
		})
	}
	assert.Zero(t, h.catalog.calls, "the service never sees a request the wrapper rejected")
}

func TestGetSeries(t *testing.T) {
	h := newHarness(t)
	h.catalog.series = service.SeriesDetail{
		ID: 81189, Slug: "breaking-bad", Name: "Breaking Bad", OriginalName: "Breaking Bad", Overview: "About a teacher", Image: "https://img/s.jpg",
		Status: service.StatusEnded, FirstAired: aired(2008, 1, 20), LastAired: aired(2013, 9, 29), AirsTime: "21:00", AirsDays: []string{"sunday"},
		OriginalCountry: "usa", OriginalLanguage: "eng", Network: "AMC", AverageRuntime: 48,
		Genres:   []service.GenreName{{ID: 12, Name: "Drama"}},
		Seasons:  []service.SeasonSummary{{Number: 0, EpisodeCount: 9}, {Number: 1, Name: "Season 1", Image: "https://img/1.jpg", EpisodeCount: 7}},
		SyncedAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
	}
	rec := send(t, h.handler, http.MethodGet, "/v1/series/81189", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, int64(81189), h.catalog.gotSeries)
	assert.Equal(t, userA, h.catalog.gotUser)
	assert.JSONEq(t, `{
	  "id":81189,"slug":"breaking-bad","name":"Breaking Bad","originalName":"Breaking Bad","overview":"About a teacher","image":"https://img/s.jpg",
	  "status":"ended","firstAired":"2008-01-20","lastAired":"2013-09-29","airsTime":"21:00","airsDays":["sunday"],
	  "originalCountry":"usa","originalLanguage":"eng","network":"AMC","averageRuntime":48,
	  "genres":[{"id":12,"name":"Drama"}],
	  "seasons":[{"number":0,"episodeCount":9},{"number":1,"name":"Season 1","image":"https://img/1.jpg","episodeCount":7}],
	  "syncedAt":"2026-11-19T12:00:00Z"}`, rec.Body.String(), "dates are plain dates, and unknown fields are left out")
}

func TestGetSeriesWithAlmostNothingKnown(t *testing.T) {
	h := newHarness(t)
	h.catalog.series = service.SeriesDetail{ID: 1, Slug: "s", Name: "N", OriginalName: "N", Status: service.StatusUnknown}
	rec := send(t, h.handler, http.MethodGet, "/v1/series/1", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"airsDays":[]`, "lists are never null")
	assert.Contains(t, rec.Body.String(), `"genres":[]`)
	assert.Contains(t, rec.Body.String(), `"seasons":[]`)
	assert.NotContains(t, rec.Body.String(), "firstAired")
}

func TestListEpisodes(t *testing.T) {
	h := newHarness(t)
	h.catalog.episodes = []service.Episode{
		{ID: 100, SeasonNumber: 1, Number: 1, AbsoluteNumber: 1, Name: "Pilot", Overview: "It begins", Aired: aired(2008, 1, 20), Runtime: 58, Image: "https://img/e.jpg", FinaleType: "season"},
		{ID: 101, SeasonNumber: 1, Number: 2, Name: "Unannounced"},
	}
	rec := send(t, h.handler, http.MethodGet, "/v1/series/81189/episodes?season=1", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.NotNil(t, h.catalog.gotSeason)
	assert.Equal(t, 1, *h.catalog.gotSeason)
	assert.Equal(t, int64(81189), h.catalog.gotSeries)
	assert.JSONEq(t, `{"episodes":[
	  {"id":100,"seasonNumber":1,"number":1,"absoluteNumber":1,"name":"Pilot","overview":"It begins","aired":"2008-01-20","runtime":58,"image":"https://img/e.jpg","finaleType":"season"},
	  {"id":101,"seasonNumber":1,"number":2,"name":"Unannounced"}]}`, rec.Body.String())

	send(t, h.handler, http.MethodGet, "/v1/series/81189/episodes", "", bearer("good-token"))
	assert.Nil(t, h.catalog.gotSeason, "no season parameter means all seasons")
	send(t, h.handler, http.MethodGet, "/v1/series/81189/episodes?season=0", "", bearer("good-token"))
	require.NotNil(t, h.catalog.gotSeason)
	assert.Equal(t, 0, *h.catalog.gotSeason, "season 0 is the specials, not 'no filter'")
}

func TestAnEmptyEpisodeListIsAnEmptyList(t *testing.T) {
	rec := send(t, newHarness(t).handler, http.MethodGet, "/v1/series/1/episodes", "", bearer("good-token"))
	assert.JSONEq(t, `{"episodes":[]}`, rec.Body.String())
}

func TestSeriesPathAndQueryParametersAreChecked(t *testing.T) {
	h := newHarness(t)
	for name, path := range map[string]string{
		"series id not a number":   "/v1/series/abc",
		"series id too large":      "/v1/series/99999999999999999999",
		"episodes id not a number": "/v1/series/abc/episodes",
		"season not a number":      "/v1/series/1/episodes?season=first",
	} {
		t.Run(name, func(t *testing.T) {
			requireError(t, send(t, h.handler, http.MethodGet, path, "", bearer("good-token")), http.StatusBadRequest, "invalid_argument")
		})
	}
	assert.Zero(t, h.catalog.calls)
}

func TestCatalogErrorsKeepTheirStatusAndCode(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"not found":    {apperr.New(apperr.NotFound, "series not found"), http.StatusNotFound, "not_found"},
		"unavailable":  {apperr.New(apperr.Unavailable, "the catalog is unavailable right now"), http.StatusServiceUnavailable, "unavailable"},
		"invalid":      {apperr.New(apperr.InvalidArgument, "query must be 2 to 100 characters"), http.StatusBadRequest, "invalid_argument"},
		"unauthorized": {apperr.New(apperr.Unauthenticated, "invalid credentials"), http.StatusUnauthorized, "unauthenticated"},
		"unexpected":   {errors.New("pq: down"), http.StatusInternalServerError, "internal"},
	}
	for name, c := range cases {
		for _, path := range []string{"/v1/search/series?query=ab", "/v1/series/1", "/v1/series/1/episodes"} {
			t.Run(name+" "+path, func(t *testing.T) {
				h := newHarness(t)
				h.catalog.err = c.err
				rec := send(t, h.handler, http.MethodGet, path, "", bearer("good-token"))
				requireError(t, rec, c.status, c.code)
				assert.NotContains(t, rec.Body.String(), "pq:")
			})
		}
	}
}

func TestTheProfileCarriesTheLanguage(t *testing.T) {
	h := newHarness(t)
	h.profile.user.Language = service.LanguageSpanish
	rec := send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"language":"es"`)

	h.profile.user.Language = "" // a user stored without one reads as English
	rec = send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token"))
	assert.Contains(t, rec.Body.String(), `"language":"en"`)

	rec = send(t, h.handler, http.MethodPatch, "/v1/me", `{"language":"es"}`, bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, h.profile.gotUpdate.Language)
	assert.Equal(t, "es", *h.profile.gotUpdate.Language)
	assert.Nil(t, h.profile.gotUpdate.DisplayName, "only the language was sent")

	send(t, h.handler, http.MethodPatch, "/v1/me", `{"displayName":"Ana"}`, bearer("good-token"))
	assert.Nil(t, h.profile.gotUpdate.Language)
}
