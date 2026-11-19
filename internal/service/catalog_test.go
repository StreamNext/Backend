package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// show builds a series as the source would return it: English and Spanish text for the series and for
// episode 1, only English for episode 2, and none for the special.
func show(id int64) SeriesData {
	return SeriesData{
		ID: id, Slug: fmt.Sprintf("show-%d", id), OriginalName: "Original Name", OriginalOverview: "Original overview",
		OriginalLanguage: "jpn", OriginalCountry: "jpn",
		Translations: map[Language]Text{
			LanguageEnglish: {Name: "English Name", Overview: "English overview"},
			LanguageSpanish: {Name: "Nombre en español", Overview: "Descripción en español"},
		},
		Image: "https://img/series.jpg", Status: StatusContinuing,
		FirstAired: day(2020, 1, 5), LastAired: day(2024, 3, 1), NextAired: day(2024, 4, 1),
		AirsTime: "21:00", AirsDays: []string{"sunday"}, Network: "NET", AverageRuntime: 45,
		Genres:  []Genre{{ID: 12, Name: "Drama", Slug: "drama"}, {ID: 15, Name: "Comedy", Slug: "comedy"}, {ID: 9999, Name: "Unmapped", Slug: "unmapped"}},
		Seasons: []Season{{ID: 1, Number: 0}, {ID: 2, Number: 1, Name: "Season 1"}, {ID: 3, Number: 2}},
		Episodes: []EpisodeData{
			{ID: 103, SeasonNumber: 2, Number: 1, OriginalName: "S2E1"},
			{ID: 101, SeasonNumber: 1, Number: 1, OriginalName: "Pilot", OriginalOverview: "Original pilot",
				Translations: map[Language]Text{LanguageEnglish: {Name: "The Pilot", Overview: "English pilot"}, LanguageSpanish: {Name: "El piloto", Overview: "Piloto en español"}},
				Aired:        day(2020, 1, 5), Runtime: 50, Image: "https://img/e1.jpg", FinaleType: "series"},
			{ID: 102, SeasonNumber: 1, Number: 2, OriginalName: "Second",
				Translations: map[Language]Text{LanguageEnglish: {Name: "The Second"}}},
			{ID: 100, SeasonNumber: 0, Number: 1, OriginalName: "Special"},
		},
		TVDBUpdatedAt: day(2024, 3, 2),
	}
}

type catalogEnv struct {
	svc    *Catalog
	source *fakeSource
	repo   *fakeCatalogRepo
	users  *fakeUsers
	clock  *clock
	ctx    context.Context
}

func newCatalogEnv() *catalogEnv {
	e := &catalogEnv{source: newFakeSource(), repo: newFakeCatalogRepo(), users: newFakeUsers(), clock: &clock{t: epoch}, ctx: context.Background()}
	e.svc = NewCatalog(CatalogDeps{Source: e.source, Repo: e.repo, Users: e.users, Now: e.clock.Now, Log: zap.NewNop()})
	return e
}

func (e *catalogEnv) user(t *testing.T, email string, lang Language) uuid.UUID {
	t.Helper()
	u, err := e.users.Create(e.ctx, NewUser{Email: email, DisplayName: "U", Timezone: "UTC"})
	require.NoError(t, err)
	u.Language = lang
	e.users.byID[u.ID] = u
	return u.ID
}

// --- series --------------------------------------------------------------------------------

func TestFirstRequestFetchesAndStoresThenServesFromOurDatabase(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	u := e.user(t, "a@b.co", LanguageEnglish)

	got, err := e.svc.Series(e.ctx, u, 1)
	require.NoError(t, err)
	assert.Equal(t, "English Name", got.Name)
	assert.Equal(t, epoch, got.SyncedAt, "stamped when stored")
	assert.Equal(t, 1, e.source.fetchCalls[1])
	assert.Contains(t, e.repo.series, int64(1))
	assert.Len(t, e.repo.episodes[1], 4, "its episodes were stored too")

	e.clock.Advance(30 * 24 * time.Hour)
	_, err = e.svc.Series(e.ctx, u, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, e.source.fetchCalls[1], "a stored series is never refreshed by the API, however old")
}

func TestAStoredSeriesDoesNotNeedTheSource(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	u := e.user(t, "a@b.co", LanguageEnglish)
	_, err := e.svc.Series(e.ctx, u, 1)
	require.NoError(t, err)

	e.source.fail["FetchSeries"] = ErrSourceUnavailable // TheTVDB goes down
	got, err := e.svc.Series(e.ctx, u, 1)
	require.NoError(t, err, "stored series do not depend on TheTVDB being up")
	assert.Equal(t, int64(1), got.ID)
}

func TestSeriesDetailInTheCallersLanguage(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	en, es := e.user(t, "en@b.co", LanguageEnglish), e.user(t, "es@b.co", LanguageSpanish)

	got, err := e.svc.Series(e.ctx, en, 1)
	require.NoError(t, err)
	assert.Equal(t, "English Name", got.Name)
	assert.Equal(t, "English overview", got.Overview)
	assert.Equal(t, "Original Name", got.OriginalName, "the original name is always there")
	assert.Equal(t, []GenreName{{12, "Drama"}, {15, "Comedy"}, {9999, "Unmapped"}}, got.Genres)

	got, err = e.svc.Series(e.ctx, es, 1)
	require.NoError(t, err)
	assert.Equal(t, "Nombre en español", got.Name)
	assert.Equal(t, "Descripción en español", got.Overview)
	assert.Equal(t, []GenreName{{12, "Drama"}, {15, "Comedia"}, {9999, "Unmapped"}}, got.Genres, "mapped genres are in Spanish (Drama happens to match); an unmapped one keeps its English name")
}

func TestSeriesDetailFields(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	got, err := e.svc.Series(e.ctx, e.user(t, "a@b.co", LanguageEnglish), 1)
	require.NoError(t, err)

	assert.Equal(t, StatusContinuing, got.Status)
	assert.Equal(t, "show-1", got.Slug)
	assert.Equal(t, []string{"sunday"}, got.AirsDays)
	assert.Equal(t, "21:00", got.AirsTime)
	assert.Equal(t, "NET", got.Network)
	assert.Equal(t, 45, got.AverageRuntime)
	assert.Equal(t, day(2024, 4, 1), got.NextAired)
	assert.Equal(t, []SeasonSummary{{Number: 0, EpisodeCount: 1}, {Number: 1, Name: "Season 1", EpisodeCount: 2}, {Number: 2, EpisodeCount: 1}}, got.Seasons)
}

func TestLanguageFallbackIsLanguageThenEnglishThenOriginal(t *testing.T) {
	tr := map[Language]Text{LanguageEnglish: {Name: "EN name", Overview: "EN overview"}, LanguageSpanish: {Name: "ES name"}}
	assert.Equal(t, "ES name", pickName(tr, "orig", LanguageSpanish))
	assert.Equal(t, "EN overview", pickOverview(tr, "orig", LanguageSpanish), "a Spanish name without a Spanish overview falls back to English for the overview only")
	assert.Equal(t, "orig", pickName(nil, "orig", LanguageSpanish))
	assert.Equal(t, "orig", pickOverview(map[Language]Text{}, "orig", LanguageEnglish))
	assert.Equal(t, "", pickName(nil, "", LanguageEnglish))
}

func TestAUserWithoutALanguageGetsEnglish(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	got, err := e.svc.Series(e.ctx, e.user(t, "a@b.co", ""), 1)
	require.NoError(t, err)
	assert.Equal(t, "English Name", got.Name)
}

func TestSeriesWithNoAirDaysGivesAnEmptyListNotNull(t *testing.T) {
	d := show(1)
	d.AirsDays = nil
	assert.NotNil(t, localizeDetail(d, LanguageEnglish).AirsDays)
}

func TestSeriesErrors(t *testing.T) {
	boom := errors.New("db down")
	cases := map[string]struct {
		setup func(*catalogEnv)
		kind  apperr.Kind
	}{
		"unknown id":         {func(e *catalogEnv) {}, apperr.NotFound},
		"source down":        {func(e *catalogEnv) { e.source.series[1] = show(1); e.source.fail["FetchSeries"] = ErrSourceUnavailable }, apperr.Unavailable},
		"catalog not set up": {func(e *catalogEnv) { e.source.series[1] = show(1); e.source.fail["FetchSeries"] = ErrSourceDisabled }, apperr.Unavailable},
		"unexpected source":  {func(e *catalogEnv) { e.source.series[1] = show(1); e.source.fail["FetchSeries"] = boom }, apperr.Internal},
		"read fails":         {func(e *catalogEnv) { e.repo.fail["GetSeries"] = boom }, apperr.Internal},
		"store fails":        {func(e *catalogEnv) { e.source.series[1] = show(1); e.repo.fail["Save"] = boom }, apperr.Internal},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCatalogEnv()
			c.setup(e)
			_, err := e.svc.Series(e.ctx, e.user(t, "a@b.co", LanguageEnglish), 1)
			requireKind(t, err, c.kind)
			assert.Empty(t, e.repo.series, "nothing is stored on failure")
		})
	}
}

func TestSeriesFailsForAnUnknownOrFailingUser(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	_, err := e.svc.Series(e.ctx, uuid.New(), 1)
	requireKind(t, err, apperr.Unauthenticated)

	u := e.user(t, "a@b.co", LanguageEnglish)
	e.users.fail["GetByID"] = errors.New("db down")
	_, err = e.svc.Series(e.ctx, u, 1)
	requireKind(t, err, apperr.Internal)
}

func TestReadingBackAfterStoringCanFail(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	u := e.user(t, "a@b.co", LanguageEnglish)
	n := 0
	boom := errors.New("db down")
	// The first read (the miss) works as normal; the re-read after Save fails.
	repo := &flakyRead{fakeCatalogRepo: e.repo, failOn: 2, err: boom, n: &n}
	e.svc.Repo = repo
	_, err := e.svc.Series(e.ctx, u, 1)
	requireKind(t, err, apperr.Internal)
}

// flakyRead fails the nth GetSeries call.
type flakyRead struct {
	*fakeCatalogRepo
	failOn int
	err    error
	n      *int
}

func (f *flakyRead) GetSeries(ctx context.Context, id int64) (SeriesData, error) {
	*f.n++
	if *f.n == f.failOn {
		return SeriesData{}, f.err
	}
	return f.fakeCatalogRepo.GetSeries(ctx, id)
}

// --- episodes ------------------------------------------------------------------------------

func TestEpisodesInOrderAndInTheCallersLanguage(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	en, es := e.user(t, "en@b.co", LanguageEnglish), e.user(t, "es@b.co", LanguageSpanish)

	got, err := e.svc.Episodes(e.ctx, en, 1, nil)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []int64{100, 101, 102, 103}, []int64{got[0].ID, got[1].ID, got[2].ID, got[3].ID}, "season 0 first, then by season and number")
	assert.Equal(t, "The Pilot", got[1].Name)
	assert.Equal(t, "English pilot", got[1].Overview)
	assert.Equal(t, "Special", got[0].Name, "no translation at all: the original")

	got, err = e.svc.Episodes(e.ctx, es, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, "El piloto", got[1].Name)
	assert.Equal(t, "The Second", got[2].Name, "no Spanish name: English")
	assert.Equal(t, "S2E1", got[3].Name)
}

func TestEpisodeFields(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	got, err := e.svc.Episodes(e.ctx, e.user(t, "a@b.co", LanguageEnglish), 1, nil)
	require.NoError(t, err)
	pilot := got[1]
	assert.Equal(t, Episode{ID: 101, SeasonNumber: 1, Number: 1, Name: "The Pilot", Overview: "English pilot", Aired: day(2020, 1, 5),
		Runtime: 50, Image: "https://img/e1.jpg", FinaleType: "series"}, pilot)
}

func TestEpisodesOfOneSeason(t *testing.T) {
	e := newCatalogEnv()
	e.source.series[1] = show(1)
	u := e.user(t, "a@b.co", LanguageEnglish)
	season := 1
	got, err := e.svc.Episodes(e.ctx, u, 1, &season)
	require.NoError(t, err)
	assert.Len(t, got, 2)

	season = 9
	got, err = e.svc.Episodes(e.ctx, u, 1, &season)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got, "an empty season is an empty list, not null")

	season = 0
	got, err = e.svc.Episodes(e.ctx, u, 1, &season)
	require.NoError(t, err)
	assert.Len(t, got, 1, "season 0 is the specials")
}

func TestEpisodesErrors(t *testing.T) {
	e := newCatalogEnv()
	u := e.user(t, "a@b.co", LanguageEnglish)
	negative := -1
	_, err := e.svc.Episodes(e.ctx, u, 1, &negative)
	requireKind(t, err, apperr.InvalidArgument)

	_, err = e.svc.Episodes(e.ctx, u, 1, nil)
	requireKind(t, err, apperr.NotFound, "unknown series")

	_, err = e.svc.Episodes(e.ctx, uuid.New(), 1, nil)
	requireKind(t, err, apperr.Unauthenticated)

	e.source.series[1] = show(1)
	e.repo.fail["GetEpisodes"] = errors.New("db down")
	_, err = e.svc.Episodes(e.ctx, u, 1, nil)
	requireKind(t, err, apperr.Internal)

	e.repo.fail = failures{}
	e.users.fail["GetByID"] = errors.New("db down")
	_, err = e.svc.Episodes(e.ctx, u, 1, nil)
	requireKind(t, err, apperr.Internal)
}

// --- search --------------------------------------------------------------------------------

func hits() SearchPage {
	return SearchPage{HasMore: true, Hits: []SearchHit{{
		ID: 1, OriginalName: "Orig", OriginalOverview: "Orig overview", Year: "2020", Network: "NET", Image: "https://img/p.jpg", Status: StatusEnded,
		Translations: map[Language]Text{LanguageEnglish: {Name: "English", Overview: "English overview"}, LanguageSpanish: {Name: "Español"}},
	}}}
}

func TestSearchLocalizesTheResults(t *testing.T) {
	e := newCatalogEnv()
	e.source.pages["bad|1"] = hits()
	en, es := e.user(t, "en@b.co", LanguageEnglish), e.user(t, "es@b.co", LanguageSpanish)

	got, err := e.svc.Search(e.ctx, en, "  bad  ", 0)
	require.NoError(t, err)
	assert.Equal(t, SearchResults{Page: 1, HasMore: true, Results: []SeriesSummary{{
		ID: 1, Name: "English", OriginalName: "Orig", Overview: "English overview", Year: "2020", Network: "NET", Image: "https://img/p.jpg", Status: StatusEnded,
	}}}, got)

	got, err = e.svc.Search(e.ctx, es, "bad", 1)
	require.NoError(t, err)
	assert.Equal(t, "Español", got.Results[0].Name)
	assert.Equal(t, "English overview", got.Results[0].Overview, "overview falls back to English")
	assert.Equal(t, 1, e.source.searchCalls, "the cache holds the source's answer, and each caller still gets their own language")
}

func TestSearchValidation(t *testing.T) {
	e := newCatalogEnv()
	u := e.user(t, "a@b.co", LanguageEnglish)
	for name, c := range map[string]struct {
		query string
		page  int
	}{
		"too short":       {"a", 1},
		"only spaces":     {"    ", 1},
		"too long":        {strings.Repeat("x", 101), 1},
		"negative page":   {"bad", -1},
		"page past limit": {"bad", 26},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.Search(e.ctx, u, c.query, c.page)
			requireKind(t, err, apperr.InvalidArgument)
		})
	}
	assert.Zero(t, e.source.searchCalls)

	_, err := e.svc.Search(e.ctx, u, "ñu", 25)
	assert.NoError(t, err, "2 characters (not bytes) and page 25 are fine")
}

func TestSearchResultsAreCachedForTenMinutes(t *testing.T) {
	e := newCatalogEnv()
	e.source.pages["bad|1"] = hits()
	e.source.pages["bad|2"] = hits()
	u := e.user(t, "a@b.co", LanguageEnglish)
	search := func(q string, page int) { _, err := e.svc.Search(e.ctx, u, q, page); require.NoError(t, err) }

	search("bad", 1)
	search("BAD", 1)
	search(" bad ", 1)
	assert.Equal(t, 1, e.source.searchCalls, "same search, whatever the case or spacing")
	search("bad", 2)
	assert.Equal(t, 2, e.source.searchCalls, "another page is another search")

	e.clock.Advance(10*time.Minute - time.Second)
	search("bad", 1)
	assert.Equal(t, 2, e.source.searchCalls)
	e.clock.Advance(2 * time.Second)
	search("bad", 1)
	assert.Equal(t, 3, e.source.searchCalls, "asked again after ten minutes")
}

func TestSearchErrorsAreNotCached(t *testing.T) {
	e := newCatalogEnv()
	u := e.user(t, "a@b.co", LanguageEnglish)
	e.source.fail["Search"] = ErrSourceUnavailable
	_, err := e.svc.Search(e.ctx, u, "bad", 1)
	requireKind(t, err, apperr.Unavailable)

	e.source.fail = failures{}
	e.source.pages["bad|1"] = hits()
	got, err := e.svc.Search(e.ctx, u, "bad", 1)
	require.NoError(t, err, "the failure was not remembered")
	assert.Len(t, got.Results, 1)

	e.source.fail["Search"] = errors.New("boom")
	_, err = e.svc.Search(e.ctx, u, "other", 1)
	requireKind(t, err, apperr.Internal)
	e.source.fail["Search"] = ErrSourceDisabled
	_, err = e.svc.Search(e.ctx, u, "another", 1)
	requireKind(t, err, apperr.Unavailable)
	_, err = e.svc.Search(e.ctx, uuid.New(), "bad", 1)
	requireKind(t, err, apperr.Unauthenticated)
}

func TestTheSearchCacheStaysBounded(t *testing.T) {
	e := newCatalogEnv()
	u := e.user(t, "a@b.co", LanguageEnglish)
	for i := 0; i < searchCacheMax+10; i++ {
		_, err := e.svc.Search(e.ctx, u, fmt.Sprintf("query number %d", i), 1)
		require.NoError(t, err)
	}
	assert.LessOrEqual(t, len(e.svc.cache.m), searchCacheMax, "full cache: drop the expired, or everything")

	// Expired entries are the first to go.
	e.clock.Advance(11 * time.Minute)
	_, err := e.svc.Search(e.ctx, u, "fresh query", 1)
	require.NoError(t, err)
	assert.Less(t, len(e.svc.cache.m), searchCacheMax)
}
