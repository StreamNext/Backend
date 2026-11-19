-- name: UpsertSeries :exec
INSERT INTO series (id, slug, original_name, original_overview, original_language, original_country, image, status,
                    first_aired, last_aired, next_aired, airs_time, airs_days, network, average_runtime, tvdb_updated_at, synced_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (id) DO UPDATE SET
    slug = EXCLUDED.slug, original_name = EXCLUDED.original_name, original_overview = EXCLUDED.original_overview,
    original_language = EXCLUDED.original_language, original_country = EXCLUDED.original_country, image = EXCLUDED.image,
    status = EXCLUDED.status, first_aired = EXCLUDED.first_aired, last_aired = EXCLUDED.last_aired,
    next_aired = EXCLUDED.next_aired, airs_time = EXCLUDED.airs_time, airs_days = EXCLUDED.airs_days,
    network = EXCLUDED.network, average_runtime = EXCLUDED.average_runtime, tvdb_updated_at = EXCLUDED.tvdb_updated_at,
    synced_at = EXCLUDED.synced_at;

-- name: DeleteSeriesTranslations :exec
DELETE FROM series_translation WHERE series_id = $1;

-- name: InsertSeriesTranslation :exec
INSERT INTO series_translation (series_id, language, name, overview) VALUES ($1, $2, $3, $4);

-- name: UpsertGenre :exec
INSERT INTO genre (id, name, slug) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug;

-- name: DeleteSeriesGenres :exec
DELETE FROM series_genre WHERE series_id = $1;

-- name: InsertSeriesGenre :exec
INSERT INTO series_genre (series_id, genre_id) VALUES ($1, $2);

-- name: DeleteSeasons :exec
DELETE FROM season WHERE series_id = $1;

-- name: InsertSeason :exec
INSERT INTO season (id, series_id, number, name, image) VALUES ($1, $2, $3, $4, $5);

-- name: UpsertEpisode :batchexec
INSERT INTO episode (id, series_id, season_number, number, absolute_number, original_name, original_overview, aired, runtime, image, finale_type)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
    series_id = EXCLUDED.series_id, season_number = EXCLUDED.season_number, number = EXCLUDED.number,
    absolute_number = EXCLUDED.absolute_number, original_name = EXCLUDED.original_name,
    original_overview = EXCLUDED.original_overview, aired = EXCLUDED.aired, runtime = EXCLUDED.runtime,
    image = EXCLUDED.image, finale_type = EXCLUDED.finale_type;

-- name: DeleteEpisodesNotIn :exec
DELETE FROM episode WHERE series_id = $1 AND NOT (id = ANY(sqlc.arg('keep')::bigint[]));

-- name: DeleteEpisodeTranslationsOfSeries :exec
DELETE FROM episode_translation WHERE episode_id IN (SELECT id FROM episode WHERE series_id = $1);

-- name: InsertEpisodeTranslation :batchexec
INSERT INTO episode_translation (episode_id, language, name, overview) VALUES ($1, $2, $3, $4);

-- name: GetSeries :one
SELECT * FROM series WHERE id = $1;

-- name: GetSeriesTranslations :many
SELECT language, name, overview FROM series_translation WHERE series_id = $1;

-- name: GetSeriesGenres :many
SELECT g.id, g.name, g.slug FROM genre g JOIN series_genre sg ON sg.genre_id = g.id
WHERE sg.series_id = $1 ORDER BY g.id;

-- name: GetSeriesSeasons :many
SELECT s.id, s.number, s.name, s.image,
       (SELECT count(*) FROM episode e WHERE e.series_id = s.series_id AND e.season_number = s.number)::int AS episode_count
FROM season s WHERE s.series_id = $1 ORDER BY s.number;

-- name: GetEpisodes :many
SELECT * FROM episode
WHERE series_id = $1 AND (sqlc.narg('season')::int IS NULL OR season_number = sqlc.narg('season')::int)
ORDER BY season_number, number;

-- name: GetEpisodeTranslations :many
SELECT t.episode_id, t.language, t.name, t.overview
FROM episode_translation t JOIN episode e ON e.id = t.episode_id
WHERE e.series_id = $1 AND (sqlc.narg('season')::int IS NULL OR e.season_number = sqlc.narg('season')::int);

-- name: DeleteSeries :execrows
DELETE FROM series WHERE id = $1;

-- name: ListSeriesIDs :many
SELECT id FROM series ORDER BY id;

-- name: ListStaleSeriesIDs :many
SELECT id FROM series WHERE synced_at < $1 ORDER BY synced_at LIMIT $2;

-- name: GetSyncState :one
SELECT value FROM sync_state WHERE key = $1;

-- name: UpsertSyncState :exec
INSERT INTO sync_state (key, value, updated_at) VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;
