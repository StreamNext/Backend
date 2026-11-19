package tvdb

import (
	"strconv"
	"strings"
	"time"

	"github.com/StreamNext/Backend/internal/service"
)

func languageOf(code string) (service.Language, bool) {
	for _, l := range languageCodes {
		if l.code == code {
			return l.lang, true
		}
	}
	return "", false
}

func (r searchResult) toHit() (service.SearchHit, bool) {
	id, err := strconv.ParseInt(r.TVDBID, 10, 64)
	if err != nil || id <= 0 {
		return service.SearchHit{}, false // not a series record we can use
	}
	tr := map[service.Language]service.Text{}
	for code, name := range r.Translations {
		if lang, ok := languageOf(code); ok {
			tr[lang] = service.Text{Name: name}
		}
	}
	for code, overview := range r.Overviews {
		if lang, ok := languageOf(code); ok {
			t := tr[lang]
			t.Overview = overview
			tr[lang] = t
		}
	}
	image := r.Thumbnail
	if image == "" {
		image = r.ImageURL
	}
	return service.SearchHit{
		ID: id, OriginalName: r.Name, OriginalOverview: r.Overview, Translations: tr,
		Year: r.Year, Network: r.Network, Image: image, Status: status(r.Status),
	}, true
}

func (s seriesExtended) toData() service.SeriesData {
	d := service.SeriesData{
		ID: s.ID, Slug: s.Slug, OriginalName: s.Name, OriginalOverview: s.Overview,
		OriginalLanguage: s.OriginalLanguage, OriginalCountry: s.OriginalCountry,
		Translations: map[service.Language]service.Text{},
		Image:        s.Image, Status: status(s.Status.Name),
		FirstAired: date(s.FirstAired), LastAired: date(s.LastAired), NextAired: date(s.NextAired),
		AirsTime: s.AirsTime, AirsDays: airsDays(s.AirsDays), Network: network(s),
		TVDBUpdatedAt: timestamp(s.LastUpdated),
	}
	if s.AverageRuntime != nil {
		d.AverageRuntime = *s.AverageRuntime
	}
	for _, t := range s.Translations.NameTranslations {
		if lang, ok := languageOf(t.Language); ok && !t.IsAlias {
			tr := d.Translations[lang]
			if tr.Name == "" { // the first real translation wins
				tr.Name = t.Name
			}
			d.Translations[lang] = tr
		}
	}
	for _, t := range s.Translations.OverviewTranslations {
		if lang, ok := languageOf(t.Language); ok && !t.IsAlias {
			tr := d.Translations[lang]
			if tr.Overview == "" {
				tr.Overview = t.Overview
			}
			d.Translations[lang] = tr
		}
	}
	for _, g := range s.Genres {
		d.Genres = append(d.Genres, service.Genre{ID: g.ID, Name: g.Name, Slug: g.Slug})
	}
	for _, se := range s.Seasons {
		if se.Type.ID == s.DefaultSeasonType { // only the series' default order; the others repeat the same episodes
			d.Seasons = append(d.Seasons, service.Season{ID: se.ID, Number: se.Number, Name: se.Name, Image: se.Image})
		}
	}
	return d
}

// mergeEpisodes combines the original-language pass with the translated passes, keyed by episode id. The
// translated passes name the same episodes; where they have no translation the name is empty.
func mergeEpisodes(original []episode, translated map[service.Language][]episode) []service.EpisodeData {
	byID := make(map[int64]*service.EpisodeData, len(original))
	out := make([]service.EpisodeData, 0, len(original))
	for _, e := range original {
		out = append(out, service.EpisodeData{
			ID: e.ID, SeasonNumber: e.SeasonNumber, Number: e.Number, AbsoluteNumber: e.AbsoluteNumber,
			OriginalName: e.Name, OriginalOverview: e.Overview, Translations: map[service.Language]service.Text{},
			Aired: date(e.Aired), Runtime: intOrZero(e.Runtime), Image: e.Image, FinaleType: stringOrEmpty(e.FinaleType),
		})
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for lang, eps := range translated {
		for _, e := range eps {
			target, ok := byID[e.ID]
			if !ok || (e.Name == "" && e.Overview == "") {
				continue
			}
			target.Translations[lang] = service.Text{Name: e.Name, Overview: e.Overview}
		}
	}
	return out
}

func status(name string) service.SeriesStatus {
	switch strings.ToLower(name) {
	case "continuing":
		return service.StatusContinuing
	case "ended":
		return service.StatusEnded
	case "upcoming":
		return service.StatusUpcoming
	}
	return service.StatusUnknown
}

func network(s seriesExtended) string {
	if s.LatestNetwork != nil && s.LatestNetwork.Name != "" {
		return s.LatestNetwork.Name
	}
	if s.OriginalNetwork != nil {
		return s.OriginalNetwork.Name
	}
	return ""
}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func airsDays(m map[string]bool) []string {
	var out []string
	for _, d := range weekdays {
		if m[d] {
			out = append(out, d)
		}
	}
	return out
}

// date reads TheTVDB's "2006-01-02"; an empty or malformed value means unknown.
func date(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// timestamp reads TheTVDB's "2006-01-02 15:04:05" (UTC).
func timestamp(s string) *time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return nil
	}
	return &t
}

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func stringOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
