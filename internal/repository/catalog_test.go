package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func resetCatalog(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), "TRUNCATE series, genre, sync_state CASCADE")
	require.NoError(t, err)
}

func d(y int, m time.Month, day int) *time.Time {
	v := time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	return &v
}

func series(id int64) service.SeriesData {
	return service.SeriesData{
		ID: id, Slug: fmt.Sprintf("show-%d", id), OriginalName: "Original", OriginalOverview: "Original overview",
		OriginalLanguage: "spa", OriginalCountry: "esp", Image: "https://img/s.jpg", Status: service.StatusContinuing,
		FirstAired: d(2020, 1, 5), LastAired: d(2024, 3, 1), NextAired: d(2024, 4, 1), AirsTime: "21:00", AirsDays: []string{"friday", "sunday"},
		Network: "Net", AverageRuntime: 45,
		Translations: map[service.Language]service.Text{
			service.LanguageEnglish: {Name: "English", Overview: "English overview"},
			service.LanguageSpanish: {Name: "Español", Overview: "Descripción"},
		},
		Genres:  []service.Genre{{ID: 12, Name: "Drama", Slug: "drama"}, {ID: 15, Name: "Comedy", Slug: "comedy"}},
		Seasons: []service.Season{{ID: id*100 + 1, Number: 0, Name: "Specials"}, {ID: id*100 + 2, Number: 1, Image: "https://img/s1.jpg"}, {ID: id*100 + 3, Number: 2}},
		Episodes: []service.EpisodeData{
			{ID: id*1000 + 1, SeasonNumber: 1, Number: 1, AbsoluteNumber: 1, OriginalName: "Pilot", OriginalOverview: "Pilot overview", Aired: d(2020, 1, 5), Runtime: 50, Image: "https://img/e1.jpg", FinaleType: "series",
				Translations: map[service.Language]service.Text{service.LanguageEnglish: {Name: "The Pilot"}, service.LanguageSpanish: {Name: "El piloto", Overview: "Piloto"}}},
			{ID: id*1000 + 2, SeasonNumber: 1, Number: 2, OriginalName: "Second"},
			{ID: id*1000 + 3, SeasonNumber: 2, Number: 1, OriginalName: "Third", Aired: nil},
			{ID: id*1000 + 4, SeasonNumber: 0, Number: 1, OriginalName: "Special"},
		},
		TVDBUpdatedAt: d(2024, 3, 2),
	}
}

func saved(t *testing.T, id int64) (*Catalog, service.SeriesData) {
	t.Helper()
	repo := NewCatalog(testPool)
	require.NoError(t, repo.Save(context.Background(), series(id), now))
	return repo, series(id)
}

func TestCatalogSaveAndGetSeriesRoundTrip(t *testing.T) {
	resetCatalog(t)
	repo, want := saved(t, 7)

	got, err := repo.GetSeries(context.Background(), 7)
	require.NoError(t, err)

	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Slug, got.Slug)
	assert.Equal(t, want.OriginalName, got.OriginalName)
	assert.Equal(t, want.OriginalOverview, got.OriginalOverview)
	assert.Equal(t, want.OriginalLanguage, got.OriginalLanguage)
	assert.Equal(t, want.OriginalCountry, got.OriginalCountry)
	assert.Equal(t, want.Image, got.Image)
	assert.Equal(t, service.StatusContinuing, got.Status)
	assert.True(t, want.FirstAired.Equal(*got.FirstAired))
	assert.True(t, want.LastAired.Equal(*got.LastAired))
	assert.True(t, want.NextAired.Equal(*got.NextAired))
	assert.Equal(t, "21:00", got.AirsTime)
	assert.Equal(t, []string{"friday", "sunday"}, got.AirsDays)
	assert.Equal(t, "Net", got.Network)
	assert.Equal(t, 45, got.AverageRuntime)
	assert.True(t, want.TVDBUpdatedAt.Equal(*got.TVDBUpdatedAt))
	assert.True(t, now.Equal(got.SyncedAt), "stamped with the time we passed")
	assert.Equal(t, want.Translations, got.Translations)
	assert.Equal(t, want.Genres, got.Genres)
	assert.Empty(t, got.Episodes, "episodes are read separately")
	assert.Equal(t, []service.Season{
		{ID: 701, Number: 0, Name: "Specials", EpisodeCount: 1},
		{ID: 702, Number: 1, Image: "https://img/s1.jpg", EpisodeCount: 2},
		{ID: 703, Number: 2, EpisodeCount: 1},
	}, got.Seasons, "ordered by number, with episode counts")
}

func TestCatalogGetSeriesThatIsNotStored(t *testing.T) {
	resetCatalog(t)
	_, err := NewCatalog(testPool).GetSeries(context.Background(), 404)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestCatalogOptionalFieldsStayEmpty(t *testing.T) {
	resetCatalog(t)
	repo := NewCatalog(testPool)
	bare := service.SeriesData{ID: 9, Slug: "bare", OriginalName: "Bare", Status: service.StatusUnknown}
	require.NoError(t, repo.Save(context.Background(), bare, now))

	got, err := repo.GetSeries(context.Background(), 9)
	require.NoError(t, err)
	assert.Nil(t, got.FirstAired)
	assert.Nil(t, got.TVDBUpdatedAt)
	assert.Equal(t, []string{}, got.AirsDays, "no air days is an empty list, never null")
	assert.Empty(t, got.Translations)
	assert.Empty(t, got.Genres)
	assert.Empty(t, got.Seasons)
	eps, err := repo.GetEpisodes(context.Background(), 9, nil)
	require.NoError(t, err)
	assert.Empty(t, eps)
}

func TestCatalogEpisodesInOrderWithTranslations(t *testing.T) {
	resetCatalog(t)
	repo, _ := saved(t, 7)
	ctx := context.Background()

	all, err := repo.GetEpisodes(ctx, 7, nil)
	require.NoError(t, err)
	require.Len(t, all, 4)
	assert.Equal(t, []int64{7004, 7001, 7002, 7003}, []int64{all[0].ID, all[1].ID, all[2].ID, all[3].ID}, "season 0 first, then by season and number")

	pilot := all[1]
	assert.Equal(t, "Pilot", pilot.OriginalName)
	assert.Equal(t, 1, pilot.AbsoluteNumber)
	assert.Equal(t, 50, pilot.Runtime)
	assert.Equal(t, "series", pilot.FinaleType)
	assert.True(t, d(2020, 1, 5).Equal(*pilot.Aired))
	assert.Equal(t, map[service.Language]service.Text{service.LanguageEnglish: {Name: "The Pilot"}, service.LanguageSpanish: {Name: "El piloto", Overview: "Piloto"}}, pilot.Translations)
	assert.Nil(t, all[3].Aired, "an unannounced episode has no date")
	assert.Empty(t, all[2].Translations)

	season := 1
	one, err := repo.GetEpisodes(ctx, 7, &season)
	require.NoError(t, err)
	assert.Len(t, one, 2)
	assert.Equal(t, map[service.Language]service.Text{service.LanguageEnglish: {Name: "The Pilot"}, service.LanguageSpanish: {Name: "El piloto", Overview: "Piloto"}}, one[0].Translations, "translations of that season only")

	season = 0
	specials, err := repo.GetEpisodes(ctx, 7, &season)
	require.NoError(t, err)
	require.Len(t, specials, 1, "season 0 is a real season, not 'no filter'")

	season = 99
	none, err := repo.GetEpisodes(ctx, 7, &season)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// A refresh must not disturb anything that references an episode (watch history, threads, later).
func TestARefreshKeepsWhatReferencesAnEpisode(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()
	repo, _ := saved(t, 7)
	_, err := testPool.Exec(ctx, "CREATE TABLE refs_test (episode_id bigint PRIMARY KEY REFERENCES episode (id) ON DELETE CASCADE)")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), "DROP TABLE IF EXISTS refs_test") })
	_, err = testPool.Exec(ctx, "INSERT INTO refs_test VALUES (7001), (7002)")
	require.NoError(t, err)

	refreshed := series(7)
	refreshed.Episodes[0].OriginalName = "Pilot, renamed"
	refreshed.Episodes = refreshed.Episodes[:3] // 7004 (the special) vanishes from the source
	refreshed.Episodes = append(refreshed.Episodes, service.EpisodeData{ID: 7005, SeasonNumber: 2, Number: 2, OriginalName: "Brand new"})
	require.NoError(t, repo.Save(ctx, refreshed, now.Add(time.Hour)))

	var n int
	require.NoError(t, testPool.QueryRow(ctx, "SELECT count(*) FROM refs_test").Scan(&n))
	assert.Equal(t, 2, n, "episodes that are still listed keep their rows, so their references survive")

	eps, _ := repo.GetEpisodes(ctx, 7, nil)
	var ids []int64
	for _, e := range eps {
		ids = append(ids, e.ID)
	}
	assert.Equal(t, []int64{7001, 7002, 7003, 7005}, ids, "the vanished one is gone and the new one is there")
	assert.Equal(t, "Pilot, renamed", eps[0].OriginalName, "changes land")

	// A vanished episode takes its references with it (cascade), as intended.
	_, err = testPool.Exec(ctx, "INSERT INTO refs_test VALUES (7003)")
	require.NoError(t, err)
	refreshed.Episodes = refreshed.Episodes[:2]
	require.NoError(t, repo.Save(ctx, refreshed, now.Add(2*time.Hour)))
	require.NoError(t, testPool.QueryRow(ctx, "SELECT count(*) FROM refs_test").Scan(&n))
	assert.Equal(t, 2, n)
}

func TestARefreshRewritesTranslationsGenresAndSeasons(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()
	repo, _ := saved(t, 7)

	changed := series(7)
	changed.Translations = map[service.Language]service.Text{service.LanguageEnglish: {Name: "New English"}} // Spanish removed
	changed.Genres = []service.Genre{{ID: 15, Name: "Comedy (renamed)", Slug: "comedy"}}                     // Drama dropped
	changed.Seasons = []service.Season{{ID: 701, Number: 0}, {ID: 799, Number: 5}}
	changed.Episodes[0].Translations = map[service.Language]service.Text{service.LanguageEnglish: {Name: "Pilot v2"}}
	changed.Status = service.StatusEnded
	changed.NextAired = nil
	require.NoError(t, repo.Save(ctx, changed, now.Add(time.Hour)))

	got, err := repo.GetSeries(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, map[service.Language]service.Text{service.LanguageEnglish: {Name: "New English"}}, got.Translations)
	assert.Equal(t, []service.Genre{{ID: 15, Name: "Comedy (renamed)", Slug: "comedy"}}, got.Genres)
	require.Len(t, got.Seasons, 2)
	assert.Equal(t, 5, got.Seasons[1].Number)
	assert.Equal(t, service.StatusEnded, got.Status)
	assert.Nil(t, got.NextAired, "a cleared date is cleared")
	assert.True(t, now.Add(time.Hour).Equal(got.SyncedAt))

	eps, _ := repo.GetEpisodes(ctx, 7, nil)
	var pilot service.EpisodeData
	for _, e := range eps {
		if e.ID == 7001 {
			pilot = e
		}
	}
	assert.Equal(t, map[service.Language]service.Text{service.LanguageEnglish: {Name: "Pilot v2"}}, pilot.Translations)
}

func TestAFailedSaveChangesNothing(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()
	repo, _ := saved(t, 7)

	bad := series(7)
	bad.OriginalName = "Should not stick"
	bad.Episodes = append(bad.Episodes, service.EpisodeData{ID: 7777, SeasonNumber: 1, Number: 9, OriginalName: "Orphan",
		Translations: map[service.Language]service.Text{"fr": {Name: "violates the language CHECK"}}})
	require.Error(t, repo.Save(ctx, bad, now.Add(time.Hour)))

	got, err := repo.GetSeries(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "Original", got.OriginalName, "the series row is unchanged")
	assert.True(t, now.Equal(got.SyncedAt))
	eps, _ := repo.GetEpisodes(ctx, 7, nil)
	assert.Len(t, eps, 4, "no half-written episode list")

	badSeries := series(8)
	badSeries.Status = "cancelled"
	assert.Error(t, repo.Save(ctx, badSeries, now), "the status CHECK")
	_, err = repo.GetSeries(ctx, 8)
	assert.ErrorIs(t, err, service.ErrNotFound)

	badTr := series(7)
	badTr.Translations = map[service.Language]service.Text{"fr": {Name: "x"}}
	assert.Error(t, repo.Save(ctx, badTr, now), "series translation language CHECK")
}

func TestCatalogDeleteTakesEverythingWithIt(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()
	repo, _ := saved(t, 7)
	saved(t, 8)

	require.NoError(t, repo.Delete(ctx, 7))
	_, err := repo.GetSeries(ctx, 7)
	assert.ErrorIs(t, err, service.ErrNotFound)

	for _, table := range []string{"series_translation", "series_genre", "season", "episode", "episode_translation"} {
		var n int
		col := map[string]string{"episode_translation": "episode_id IN (7001, 7002, 7003, 7004)", "series_genre": "series_id = 7", "series_translation": "series_id = 7", "season": "series_id = 7", "episode": "series_id = 7"}[table]
		require.NoError(t, testPool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+col).Scan(&n))
		assert.Zero(t, n, table+" rows cascade")
	}
	_, err = repo.GetSeries(ctx, 8)
	assert.NoError(t, err, "other series are untouched")
	var genres int
	require.NoError(t, testPool.QueryRow(ctx, "SELECT count(*) FROM genre").Scan(&genres))
	assert.Equal(t, 2, genres, "genres are shared and stay")

	assert.NoError(t, repo.Delete(ctx, 7), "deleting a series that is gone is fine")
}

func TestCatalogIDsAndStale(t *testing.T) {
	resetCatalog(t)
	ctx, repo := context.Background(), NewCatalog(testPool)
	for i, id := range []int64{30, 10, 20} {
		require.NoError(t, repo.Save(ctx, series(id), now.Add(-time.Duration(i)*24*time.Hour)))
	}
	ids, err := repo.IDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{10, 20, 30}, ids)

	stale, err := repo.Stale(ctx, now.Add(-12*time.Hour), 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{20, 10}, stale, "synced before the cutoff, oldest first: 20 is 48h old, 10 is 24h old, 30 is fresh")
	limited, err := repo.Stale(ctx, now.Add(-12*time.Hour), 1)
	require.NoError(t, err)
	assert.Equal(t, []int64{20}, limited, "the oldest one, within the limit")

	none, err := repo.Stale(ctx, now.Add(-100*24*time.Hour), 10)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestCatalogSaveOfALargeSeries(t *testing.T) {
	resetCatalog(t)
	ctx, repo := context.Background(), NewCatalog(testPool)
	big := series(5)
	big.Episodes = nil
	for i := int64(1); i <= 3000; i++ {
		big.Episodes = append(big.Episodes, service.EpisodeData{ID: 50000 + i, SeasonNumber: int(i/100) + 1, Number: int(i%100) + 1, OriginalName: fmt.Sprintf("Episode %d", i),
			Translations: map[service.Language]service.Text{service.LanguageEnglish: {Name: fmt.Sprintf("English %d", i)}}})
	}
	start := time.Now()
	require.NoError(t, repo.Save(ctx, big, now))
	assert.Less(t, time.Since(start), 20*time.Second, "a soap opera's worth of episodes saves in one go")
	eps, err := repo.GetEpisodes(ctx, 5, nil)
	require.NoError(t, err)
	assert.Len(t, eps, 3000)
	require.NoError(t, repo.Save(ctx, big, now.Add(time.Hour)), "and refreshes")
}

func TestConcurrentSavesOfTheSameSeriesAreHarmless(t *testing.T) {
	resetCatalog(t)
	ctx, repo := context.Background(), NewCatalog(testPool)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for round := 0; round < 3; round++ { // the API's first-open and the sync job can race like this
				errs <- repo.Save(ctx, series(7), now.Add(time.Duration(i*10+round)*time.Second))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	eps, err := repo.GetEpisodes(ctx, 7, nil)
	require.NoError(t, err)
	assert.Len(t, eps, 4, "one consistent copy, whichever save won")
}

func TestCatalogErrorsPassThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewCatalog(testPool)
	_, err := repo.GetSeries(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
	_, err = repo.GetEpisodes(ctx, 1, nil)
	assert.Error(t, err)
	assert.Error(t, repo.Save(ctx, series(1), now))
	assert.Error(t, repo.Delete(ctx, 1))
	_, err = repo.IDs(ctx)
	assert.Error(t, err)
	_, err = repo.Stale(ctx, now, 1)
	assert.Error(t, err)
}

func TestSyncStateGetAndSet(t *testing.T) {
	resetCatalog(t)
	ctx, repo := context.Background(), NewSyncState(testPool)

	_, err := repo.Get(ctx, "cursor")
	assert.ErrorIs(t, err, service.ErrNotFound)

	require.NoError(t, repo.Set(ctx, "cursor", "first", now))
	got, err := repo.Get(ctx, "cursor")
	require.NoError(t, err)
	assert.Equal(t, "first", got)

	require.NoError(t, repo.Set(ctx, "cursor", "second", now.Add(time.Hour)))
	got, _ = repo.Get(ctx, "cursor")
	assert.Equal(t, "second", got, "set again overwrites")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = repo.Get(cancelled, "cursor")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
	assert.Error(t, repo.Set(cancelled, "cursor", "x", now))
}
