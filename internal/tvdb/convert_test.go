package tvdb

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func TestStatusMapping(t *testing.T) {
	for in, want := range map[string]service.SeriesStatus{
		"Continuing": service.StatusContinuing, "Ended": service.StatusEnded, "Upcoming": service.StatusUpcoming,
		"ended": service.StatusEnded, "": service.StatusUnknown, "Cancelled": service.StatusUnknown,
	} {
		assert.Equal(t, want, status(in), in)
	}
}

func TestDatesAndTimestamps(t *testing.T) {
	assert.Equal(t, time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), *date("2024-02-29"))
	assert.Nil(t, date(""))
	assert.Nil(t, date("0000-00-00"))
	assert.Nil(t, date("soon"))
	assert.Equal(t, time.Date(2026, 10, 28, 8, 46, 22, 0, time.UTC), *timestamp("2026-10-28 08:46:22"))
	assert.Nil(t, timestamp(""))
}

func TestAirDaysComeOutInWeekOrder(t *testing.T) {
	assert.Equal(t, []string{"monday", "friday", "sunday"}, airsDays(map[string]bool{"sunday": true, "friday": true, "monday": true, "tuesday": false}))
	assert.Empty(t, airsDays(nil))
}

func TestNetworkPrefersTheLatest(t *testing.T) {
	assert.Equal(t, "New", network(seriesExtended{LatestNetwork: &namedValue{"New"}, OriginalNetwork: &namedValue{"Old"}}))
	assert.Equal(t, "Old", network(seriesExtended{LatestNetwork: &namedValue{""}, OriginalNetwork: &namedValue{"Old"}}))
	assert.Equal(t, "Old", network(seriesExtended{OriginalNetwork: &namedValue{"Old"}}))
	assert.Equal(t, "", network(seriesExtended{}))
}

func TestStringMapToleratesAnythingElse(t *testing.T) {
	var r searchResult
	require.NoError(t, json.Unmarshal([]byte(`{"translations":["eng","spa"],"overviews":null}`), &r), "TheTVDB sometimes sends a list or null")
	assert.Empty(t, r.Translations)
	require.NoError(t, json.Unmarshal([]byte(`{"translations":{"eng":"A","fra":"B"}}`), &r))
	assert.Equal(t, "A", r.Translations["eng"])
}

func TestSearchHitOnlyKeepsOurLanguagesAndPrefersTheThumbnail(t *testing.T) {
	hit, ok := searchResult{TVDBID: "7", Name: "N", Translations: stringMap{"eng": "E", "fra": "F"}, Overviews: stringMap{"spa": "S"},
		ImageURL: "full.jpg", Thumbnail: "thumb.jpg"}.toHit()
	require.True(t, ok)
	assert.Equal(t, map[service.Language]service.Text{service.LanguageEnglish: {Name: "E"}, service.LanguageSpanish: {Overview: "S"}}, hit.Translations)
	assert.Equal(t, "thumb.jpg", hit.Image)

	hit, _ = searchResult{TVDBID: "7", ImageURL: "full.jpg"}.toHit()
	assert.Equal(t, "full.jpg", hit.Image)

	for _, bad := range []string{"", "x", "0", "-5"} {
		_, ok := searchResult{TVDBID: bad}.toHit()
		assert.False(t, ok, bad)
	}
}

func TestSeriesWithMissingOptionalFields(t *testing.T) {
	d := seriesExtended{ID: 1, Name: "N"}.toData()
	assert.Equal(t, service.StatusUnknown, d.Status)
	assert.Zero(t, d.AverageRuntime)
	assert.Nil(t, d.FirstAired)
	assert.Nil(t, d.TVDBUpdatedAt)
	assert.Empty(t, d.Seasons)
}

func TestMergeEpisodesIgnoresStrangersAndEmptyTranslations(t *testing.T) {
	original := []episode{{ID: 1, Name: "Orig", Overview: "O", Runtime: intPtr(40), FinaleType: strPtr("season"), Aired: "2020-01-02"}, {ID: 2, Name: "Two"}}
	translated := map[service.Language][]episode{
		service.LanguageEnglish: {{ID: 1, Name: "English", Overview: "EO"}, {ID: 2}, {ID: 99, Name: "an episode we don't have"}},
		service.LanguageSpanish: {{ID: 1, Name: "", Overview: ""}},
	}
	got := mergeEpisodes(original, translated)
	require.Len(t, got, 2)
	assert.Equal(t, service.Text{Name: "English", Overview: "EO"}, got[0].Translations[service.LanguageEnglish])
	assert.NotContains(t, got[0].Translations, service.LanguageSpanish, "an empty translation is not stored")
	assert.Empty(t, got[1].Translations)
	assert.Equal(t, 40, got[0].Runtime)
	assert.Equal(t, "season", got[0].FinaleType)
	assert.Equal(t, 0, got[1].Runtime)
	assert.Equal(t, "", got[1].FinaleType)
}

func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }

// TheTVDB mixes a series' aliases into its name translations, flagged isAlias. Each rule below is a
// separate defense, so each is tested alone (the recorded fixtures happen to satisfy both at once).
func seriesWithNames(names ...[3]string) seriesExtended { // {name, language, "alias" or ""}
	var s seriesExtended
	for _, n := range names {
		s.Translations.NameTranslations = append(s.Translations.NameTranslations, struct {
			Name     string `json:"name"`
			Language string `json:"language"`
			IsAlias  bool   `json:"isAlias"`
		}{Name: n[0], Language: n[1], IsAlias: n[2] == "alias"})
	}
	return s
}

func TestAnAliasIsNeverATranslationEvenWhenItComesFirst(t *testing.T) {
	d := seriesWithNames([3]string{"An Alias", "eng", "alias"}, [3]string{"Real Name", "eng", ""}).toData()
	assert.Equal(t, "Real Name", d.Translations[service.LanguageEnglish].Name)

	d = seriesWithNames([3]string{"Only An Alias", "spa", "alias"}).toData()
	assert.Empty(t, d.Translations[service.LanguageSpanish].Name, "a language that has only aliases has no translation")
}

func TestTheFirstRealTranslationWins(t *testing.T) {
	d := seriesWithNames([3]string{"First", "eng", ""}, [3]string{"Second", "eng", ""}).toData()
	assert.Equal(t, "First", d.Translations[service.LanguageEnglish].Name)

	s := seriesExtended{}
	for _, o := range []string{"First overview", "Second overview"} {
		s.Translations.OverviewTranslations = append(s.Translations.OverviewTranslations, struct {
			Overview string `json:"overview"`
			Language string `json:"language"`
			IsAlias  bool   `json:"isAlias"`
		}{Overview: o, Language: "eng"})
	}
	assert.Equal(t, "First overview", s.toData().Translations[service.LanguageEnglish].Overview)
}

func TestLanguagesOtherThanOursAreIgnored(t *testing.T) {
	d := seriesWithNames([3]string{"Français", "fra", ""}, [3]string{"English", "eng", ""}).toData()
	assert.Len(t, d.Translations, 1)
}
