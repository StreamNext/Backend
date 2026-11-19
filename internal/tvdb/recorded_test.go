package tvdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/tvdb/stub"
)

func TestSearchOnRecordedData(t *testing.T) {
	page, err := recorded(t).Search(ctx(), "breaking bad", 1)
	require.NoError(t, err)
	require.NotEmpty(t, page.Hits)
	first := page.Hits[0]
	assert.Equal(t, int64(81189), first.ID)
	assert.Equal(t, "Breaking Bad", first.OriginalName)
	assert.Equal(t, service.StatusEnded, first.Status)
	assert.Equal(t, "2008", first.Year)
	assert.Equal(t, "AMC", first.Network)
	assert.Contains(t, first.Image, "https://artworks.thetvdb.com/")
	assert.Equal(t, "Overview placeholder", first.OriginalOverview, "descriptive text is a placeholder in the fixtures")
	assert.False(t, page.HasMore, "the recorded page holds every result")
}

func TestSearchSeesTheSpanishAndEnglishNames(t *testing.T) {
	page, err := recorded(t).Search(ctx(), "La Casa de Papel", 1)
	require.NoError(t, err)
	var hit service.SearchHit
	for _, h := range page.Hits {
		if h.ID == 327417 {
			hit = h
		}
	}
	require.Equal(t, int64(327417), hit.ID)
	assert.Equal(t, "La casa de papel", hit.OriginalName)
	assert.Equal(t, "Money Heist", hit.Translations[service.LanguageEnglish].Name)
	assert.Equal(t, "La casa de papel", hit.Translations[service.LanguageSpanish].Name)
}

func TestSearchWithNoMatchIsEmpty(t *testing.T) {
	page, err := recorded(t).Search(ctx(), "zzzz nothing like this", 1)
	require.NoError(t, err)
	assert.Empty(t, page.Hits)
	assert.False(t, page.HasMore)

	page, err = recorded(t).Search(ctx(), "breaking bad", 2)
	require.NoError(t, err)
	assert.Empty(t, page.Hits, "the second page of the stub is empty")
}

func TestEnglishOriginalSeriesOnRecordedData(t *testing.T) {
	d, err := recorded(t).FetchSeries(ctx(), 81189)
	require.NoError(t, err)

	assert.Equal(t, int64(81189), d.ID)
	assert.Equal(t, "breaking-bad", d.Slug)
	assert.Equal(t, "Breaking Bad", d.OriginalName)
	assert.Equal(t, "eng", d.OriginalLanguage)
	assert.Equal(t, "usa", d.OriginalCountry)
	assert.Equal(t, service.StatusEnded, d.Status)
	assert.Equal(t, "AMC", d.Network)
	assert.Equal(t, []string{"sunday"}, d.AirsDays)
	assert.Equal(t, "21:00", d.AirsTime)
	assert.Equal(t, 48, d.AverageRuntime)
	assert.Equal(t, time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC), *d.FirstAired)
	require.NotNil(t, d.TVDBUpdatedAt)
	assert.Equal(t, "Breaking Bad", d.Translations[service.LanguageEnglish].Name)
	assert.Equal(t, "Overview placeholder (eng)", d.Translations[service.LanguageEnglish].Overview)

	var names []string
	for _, g := range d.Genres {
		names = append(names, g.Name)
	}
	assert.Contains(t, names, "Drama")
}

func TestOnlyTheDefaultSeasonOrderIsKept(t *testing.T) {
	var raw struct {
		Data seriesExtended `json:"data"`
	}
	fixture(t, "series_327417_extended.json", &raw)
	all, defaults := map[int]bool{}, 0
	for _, s := range raw.Data.Seasons {
		all[s.Number] = true
		if s.Type.ID == raw.Data.DefaultSeasonType {
			defaults++
		}
	}
	require.Greater(t, len(raw.Data.Seasons), defaults, "the recording really does mix season orders")

	d, err := recorded(t).FetchSeries(ctx(), 327417)
	require.NoError(t, err)
	assert.Len(t, d.Seasons, defaults)
	seen := map[int]bool{}
	for _, s := range d.Seasons {
		assert.False(t, seen[s.Number], "season %d appears once", s.Number)
		seen[s.Number] = true
	}
	assert.True(t, seen[0] && seen[1], "specials and season 1")
}

func TestASpanishOriginalSeriesOnRecordedData(t *testing.T) {
	d, err := recorded(t).FetchSeries(ctx(), 327417)
	require.NoError(t, err)

	assert.Equal(t, "La casa de papel", d.OriginalName)
	assert.Equal(t, "spa", d.OriginalLanguage)
	assert.Equal(t, "esp", d.OriginalCountry)
	assert.Equal(t, "Netflix", d.Network)
	assert.Equal(t, []string{"friday"}, d.AirsDays)
	// The real data mixes aliases ("Papírový dům", "La casa de papel") into the English name list, flagged
	// isAlias. The first real translation must win, not the last entry.
	assert.Equal(t, "Money Heist", d.Translations[service.LanguageEnglish].Name)
	assert.Equal(t, "La casa de papel", d.Translations[service.LanguageSpanish].Name)
}

func TestEpisodesCarryEveryLanguageOnRecordedData(t *testing.T) {
	d, err := recorded(t).FetchSeries(ctx(), 327417)
	require.NoError(t, err)

	var pass struct {
		Data struct {
			Episodes []episode `json:"episodes"`
		} `json:"data"`
	}
	fixture(t, "series_327417_episodes_original.json", &pass)
	require.Len(t, d.Episodes, len(pass.Data.Episodes), "every episode of the recording comes through")

	byID := map[int64]service.EpisodeData{}
	for _, e := range d.Episodes {
		byID[e.ID] = e
	}
	pilot := byID[6056377]
	assert.Equal(t, 1, pilot.SeasonNumber)
	assert.Equal(t, 1, pilot.Number)
	assert.Equal(t, "Efectuar lo acordado", pilot.OriginalName)
	assert.Equal(t, "Do as Planned", pilot.Translations[service.LanguageEnglish].Name)
	assert.Equal(t, "Efectuar lo acordado", pilot.Translations[service.LanguageSpanish].Name)
	assert.Equal(t, time.Date(2017, 5, 2, 0, 0, 0, 0, time.UTC), *pilot.Aired)
	assert.Equal(t, 80, pilot.Runtime)
	assert.NotEmpty(t, pilot.Image)
}

func TestSpecialsAreKeptAsSeasonZero(t *testing.T) {
	d, err := recorded(t).FetchSeries(ctx(), 81189)
	require.NoError(t, err)
	specials := 0
	for _, e := range d.Episodes {
		if e.SeasonNumber == 0 {
			specials++
		}
	}
	assert.Positive(t, specials)
}

func TestAnUnknownSeriesIsNotFound(t *testing.T) {
	_, err := recorded(t).FetchSeries(ctx(), 999999999)
	requireErr(t, err, service.ErrSourceNotFound)
}

func TestTheStubDemandsItsToken(t *testing.T) {
	c, _ := newClient(t, stub.Handler(), "any-key")
	_, err := c.Raw(ctx(), "/search?query=breaking+bad")
	require.NoError(t, err, "logged in through the stub")

	srv := stubServer(t)
	resp, err := srv.Client().Get(srv.URL + "/v4/search?query=x")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 401, resp.StatusCode, "no token, no data")
	assert.Equal(t, "breaking_bad", stub.Slug(" Breaking  Bad!! "))
}
