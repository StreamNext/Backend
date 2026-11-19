package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

type Catalog struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewCatalog(pool *pgxpool.Pool) *Catalog { return &Catalog{pool: pool, q: sqlc.New(pool)} }

// GetSeries reads a stored series without its episodes; each season carries its episode count.
func (r *Catalog) GetSeries(ctx context.Context, id int64) (service.SeriesData, error) {
	row, err := r.q.GetSeries(ctx, id)
	if err != nil {
		return service.SeriesData{}, notFound(err)
	}
	d := service.SeriesData{
		ID: row.ID, Slug: row.Slug, OriginalName: row.OriginalName, OriginalOverview: row.OriginalOverview,
		OriginalLanguage: row.OriginalLanguage, OriginalCountry: row.OriginalCountry, Image: row.Image,
		Status:     service.SeriesStatus(row.Status),
		FirstAired: row.FirstAired, LastAired: row.LastAired, NextAired: row.NextAired,
		AirsTime: row.AirsTime, AirsDays: row.AirsDays, Network: row.Network, AverageRuntime: int(row.AverageRuntime),
		TVDBUpdatedAt: row.TvdbUpdatedAt, SyncedAt: row.SyncedAt, Translations: map[service.Language]service.Text{},
	}

	translations, err := r.q.GetSeriesTranslations(ctx, id)
	if err != nil {
		return service.SeriesData{}, err
	}
	for _, t := range translations {
		d.Translations[service.Language(t.Language)] = service.Text{Name: t.Name, Overview: t.Overview}
	}
	genres, err := r.q.GetSeriesGenres(ctx, id)
	if err != nil {
		return service.SeriesData{}, err
	}
	for _, g := range genres {
		d.Genres = append(d.Genres, service.Genre{ID: g.ID, Name: g.Name, Slug: g.Slug})
	}
	seasons, err := r.q.GetSeriesSeasons(ctx, id)
	if err != nil {
		return service.SeriesData{}, err
	}
	for _, s := range seasons {
		d.Seasons = append(d.Seasons, service.Season{ID: s.ID, Number: int(s.Number), Name: s.Name, Image: s.Image, EpisodeCount: int(s.EpisodeCount)})
	}
	return d, nil
}

// GetEpisodes reads a series' episodes in order with their translations, optionally one season only.
func (r *Catalog) GetEpisodes(ctx context.Context, seriesID int64, season *int) ([]service.EpisodeData, error) {
	var s pgtype.Int4
	if season != nil {
		s = pgtype.Int4{Int32: int32(*season), Valid: true}
	}
	rows, err := r.q.GetEpisodes(ctx, sqlc.GetEpisodesParams{SeriesID: seriesID, Season: s})
	if err != nil {
		return nil, err
	}
	translations, err := r.q.GetEpisodeTranslations(ctx, sqlc.GetEpisodeTranslationsParams{SeriesID: seriesID, Season: s})
	if err != nil {
		return nil, err
	}
	byEpisode := map[int64]map[service.Language]service.Text{}
	for _, t := range translations {
		if byEpisode[t.EpisodeID] == nil {
			byEpisode[t.EpisodeID] = map[service.Language]service.Text{}
		}
		byEpisode[t.EpisodeID][service.Language(t.Language)] = service.Text{Name: t.Name, Overview: t.Overview}
	}
	out := make([]service.EpisodeData, 0, len(rows))
	for _, e := range rows {
		out = append(out, service.EpisodeData{
			ID: e.ID, SeasonNumber: int(e.SeasonNumber), Number: int(e.Number), AbsoluteNumber: int(e.AbsoluteNumber),
			OriginalName: e.OriginalName, OriginalOverview: e.OriginalOverview, Translations: byEpisode[e.ID],
			Aired: e.Aired, Runtime: int(e.Runtime), Image: e.Image, FinaleType: e.FinaleType,
		})
	}
	return out, nil
}

// Save stores a series in one transaction. Translations, genres and seasons are rewritten. Episodes are
// upserted by their TheTVDB id (never deleted and re-created), so anything that references an episode survives
// a refresh; only episodes the source no longer lists are removed. If any step fails nothing changes.
func (r *Catalog) Save(ctx context.Context, d service.SeriesData, now time.Time) error {
	return inTx(ctx, r.pool, r.q, func(q *sqlc.Queries) error {
		days := d.AirsDays
		if days == nil {
			days = []string{}
		}
		if err := q.UpsertSeries(ctx, sqlc.UpsertSeriesParams{
			ID: d.ID, Slug: d.Slug, OriginalName: d.OriginalName, OriginalOverview: d.OriginalOverview,
			OriginalLanguage: d.OriginalLanguage, OriginalCountry: d.OriginalCountry, Image: d.Image, Status: string(d.Status),
			FirstAired: d.FirstAired, LastAired: d.LastAired, NextAired: d.NextAired, AirsTime: d.AirsTime, AirsDays: days,
			Network: d.Network, AverageRuntime: int32(d.AverageRuntime), TvdbUpdatedAt: d.TVDBUpdatedAt, SyncedAt: now,
		}); err != nil {
			return err
		}

		if err := q.DeleteSeriesTranslations(ctx, d.ID); err != nil {
			return err
		}
		for lang, t := range d.Translations {
			if t.Name == "" && t.Overview == "" {
				continue
			}
			if err := q.InsertSeriesTranslation(ctx, sqlc.InsertSeriesTranslationParams{SeriesID: d.ID, Language: string(lang), Name: t.Name, Overview: t.Overview}); err != nil {
				return err
			}
		}

		if err := q.DeleteSeriesGenres(ctx, d.ID); err != nil {
			return err
		}
		for _, g := range d.Genres {
			if err := q.UpsertGenre(ctx, sqlc.UpsertGenreParams{ID: g.ID, Name: g.Name, Slug: g.Slug}); err != nil {
				return err
			}
			if err := q.InsertSeriesGenre(ctx, sqlc.InsertSeriesGenreParams{SeriesID: d.ID, GenreID: g.ID}); err != nil {
				return err
			}
		}

		if err := q.DeleteSeasons(ctx, d.ID); err != nil {
			return err
		}
		for _, s := range d.Seasons {
			if err := q.InsertSeason(ctx, sqlc.InsertSeasonParams{ID: s.ID, SeriesID: d.ID, Number: int32(s.Number), Name: s.Name, Image: s.Image}); err != nil {
				return err
			}
		}
		return saveEpisodes(ctx, q, d)
	})
}

func saveEpisodes(ctx context.Context, q *sqlc.Queries, d service.SeriesData) error {
	keep := make([]int64, 0, len(d.Episodes))
	upserts := make([]sqlc.UpsertEpisodeParams, 0, len(d.Episodes))
	var translations []sqlc.InsertEpisodeTranslationParams
	for _, e := range d.Episodes {
		keep = append(keep, e.ID)
		upserts = append(upserts, sqlc.UpsertEpisodeParams{
			ID: e.ID, SeriesID: d.ID, SeasonNumber: int32(e.SeasonNumber), Number: int32(e.Number), AbsoluteNumber: int32(e.AbsoluteNumber),
			OriginalName: e.OriginalName, OriginalOverview: e.OriginalOverview, Aired: e.Aired, Runtime: int32(e.Runtime),
			Image: e.Image, FinaleType: e.FinaleType,
		})
		for lang, t := range e.Translations {
			if t.Name != "" || t.Overview != "" {
				translations = append(translations, sqlc.InsertEpisodeTranslationParams{EpisodeID: e.ID, Language: string(lang), Name: t.Name, Overview: t.Overview})
			}
		}
	}
	if err := q.DeleteEpisodesNotIn(ctx, sqlc.DeleteEpisodesNotInParams{SeriesID: d.ID, Keep: keep}); err != nil {
		return err
	}
	if len(upserts) > 0 {
		if err := runBatch(q.UpsertEpisode(ctx, upserts)); err != nil {
			return err
		}
	}
	if err := q.DeleteEpisodeTranslationsOfSeries(ctx, d.ID); err != nil {
		return err
	}
	if len(translations) > 0 {
		return runBatch(q.InsertEpisodeTranslation(ctx, translations))
	}
	return nil
}

// runBatch executes a pgx batch and returns the first error of any statement in it.
func runBatch(br interface {
	Exec(func(int, error))
	Close() error
}) error {
	var first error
	br.Exec(func(_ int, err error) {
		if err != nil && first == nil {
			first = err
		}
	})
	if err := br.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

func (r *Catalog) Delete(ctx context.Context, id int64) error {
	_, err := r.q.DeleteSeries(ctx, id) // children cascade; a missing series is fine
	return err
}

func (r *Catalog) IDs(ctx context.Context) ([]int64, error) { return r.q.ListSeriesIDs(ctx) }

func (r *Catalog) Stale(ctx context.Context, before time.Time, limit int) ([]int64, error) {
	return r.q.ListStaleSeriesIDs(ctx, sqlc.ListStaleSeriesIDsParams{SyncedAt: before, Limit: int32(limit)})
}

// SyncState is the small key/value store background jobs keep their cursors in.
type SyncState struct{ q *sqlc.Queries }

func NewSyncState(db sqlc.DBTX) *SyncState { return &SyncState{q: sqlc.New(db)} }

func (r *SyncState) Get(ctx context.Context, key string) (string, error) {
	v, err := r.q.GetSyncState(ctx, key)
	if err != nil {
		return "", notFound(err)
	}
	return v, nil
}

func (r *SyncState) Set(ctx context.Context, key, value string, now time.Time) error {
	return r.q.UpsertSyncState(ctx, sqlc.UpsertSyncStateParams{Key: key, Value: value, UpdatedAt: now})
}
