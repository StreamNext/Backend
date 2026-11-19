package service

import (
	"context"
	"errors"
	"time"
)

// The catalog is TheTVDB's series data, kept in our own database for the series people open. See
// docs/features/catalog.md.

var (
	ErrSourceNotFound    = errors.New("catalog source does not know that id")
	ErrSourceUnavailable = errors.New("catalog source is unavailable")
	ErrSourceDisabled    = errors.New("catalog source is not configured")
)

type Language string

const (
	LanguageEnglish Language = "en"
	LanguageSpanish Language = "es"
)

func (l Language) Valid() bool { return l == LanguageEnglish || l == LanguageSpanish }

type SeriesStatus string

const (
	StatusContinuing SeriesStatus = "continuing"
	StatusEnded      SeriesStatus = "ended"
	StatusUpcoming   SeriesStatus = "upcoming"
	StatusUnknown    SeriesStatus = "unknown"
)

// Text is a name and overview in one language.
type Text struct{ Name, Overview string }

type Genre struct {
	ID         int64
	Name, Slug string // English, as TheTVDB names them
}

type Season struct {
	ID           int64
	Number       int // 0 = specials
	Name, Image  string
	EpisodeCount int // filled in when read from the database
}

// EpisodeData is an episode with its text in every language we keep.
type EpisodeData struct {
	ID                                   int64
	SeasonNumber, Number, AbsoluteNumber int
	OriginalName, OriginalOverview       string
	Translations                         map[Language]Text
	Aired                                *time.Time // a date; nil while unannounced
	Runtime                              int        // minutes, 0 = unknown
	Image, FinaleType                    string
}

// SeriesData is a series as fetched from TheTVDB and as stored. Episodes is filled when fetching and
// saving, and empty when read back for a detail page (episodes are read separately).
type SeriesData struct {
	ID                                                                int64
	Slug                                                              string
	OriginalName, OriginalOverview, OriginalLanguage, OriginalCountry string
	Translations                                                      map[Language]Text
	Image                                                             string
	Status                                                            SeriesStatus
	FirstAired, LastAired, NextAired                                  *time.Time
	AirsTime                                                          string // local to the series' country
	AirsDays                                                          []string
	Network                                                           string
	AverageRuntime                                                    int
	Genres                                                            []Genre
	Seasons                                                           []Season
	Episodes                                                          []EpisodeData
	TVDBUpdatedAt                                                     *time.Time
	SyncedAt                                                          time.Time
}

// SearchHit is one search result, with its text in every language the source returned.
type SearchHit struct {
	ID                             int64
	OriginalName, OriginalOverview string
	Translations                   map[Language]Text
	Year, Network, Image           string
	Status                         SeriesStatus
}

type SearchPage struct {
	Hits    []SearchHit
	HasMore bool
}

// SeriesUpdate is one entry of the source's changes feed.
type SeriesUpdate struct {
	ID      int64
	Deleted bool
}

// CatalogSource is where series data comes from (TheTVDB). It returns ErrSourceNotFound,
// ErrSourceUnavailable or ErrSourceDisabled (possibly wrapped) when it cannot answer.
type CatalogSource interface {
	Search(ctx context.Context, query string, page int) (SearchPage, error)
	FetchSeries(ctx context.Context, id int64) (SeriesData, error)
	// SeriesUpdates lists series changed since the given time, following every page.
	SeriesUpdates(ctx context.Context, since time.Time) ([]SeriesUpdate, error)
}

type CatalogRepository interface {
	// GetSeries returns a stored series without its episodes; ErrNotFound if it is not stored.
	GetSeries(ctx context.Context, id int64) (SeriesData, error)
	// GetEpisodes returns a stored series' episodes in order, optionally one season only.
	GetEpisodes(ctx context.Context, seriesID int64, season *int) ([]EpisodeData, error)
	// Save stores the series in one transaction: translations, genres and seasons are rewritten and
	// episodes are upserted by id, and episodes the source no longer lists are removed. It sets SyncedAt to now.
	Save(ctx context.Context, d SeriesData, now time.Time) error
	// Delete removes a series and everything under it; a missing series is not an error.
	Delete(ctx context.Context, id int64) error
	IDs(ctx context.Context) ([]int64, error)
	// Stale lists up to limit series last synced before the given time, oldest first.
	Stale(ctx context.Context, before time.Time, limit int) ([]int64, error)
}

type SyncStateRepository interface {
	Get(ctx context.Context, key string) (string, error) // ErrNotFound
	Set(ctx context.Context, key, value string, now time.Time) error
}

// --- what the API returns, in the caller's language --------------------------------------------

type SeriesSummary struct {
	ID                           int64
	Name, OriginalName, Overview string
	Year, Network, Image         string
	Status                       SeriesStatus
}

type SearchResults struct {
	Results []SeriesSummary
	Page    int
	HasMore bool
}

type GenreName struct {
	ID   int64
	Name string
}

type SeasonSummary struct {
	Number       int
	Name, Image  string
	EpisodeCount int
}

type SeriesDetail struct {
	ID                                 int64
	Slug, Name, OriginalName, Overview string
	Image                              string
	Status                             SeriesStatus
	FirstAired, LastAired, NextAired   *time.Time
	AirsTime                           string
	AirsDays                           []string
	OriginalCountry, OriginalLanguage  string
	Network                            string
	AverageRuntime                     int
	Genres                             []GenreName
	Seasons                            []SeasonSummary
	SyncedAt                           time.Time
}

type Episode struct {
	ID                                   int64
	SeasonNumber, Number, AbsoluteNumber int
	Name, Overview                       string
	Aired                                *time.Time
	Runtime                              int
	Image, FinaleType                    string
}
