-- +goose Up
ALTER TABLE users ADD COLUMN language text NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'es'));

-- A series we hold in full: someone opened it, and the tvdb-sync job keeps it fresh. Ids are TheTVDB's.
CREATE TABLE series (
    id                bigint      PRIMARY KEY,
    slug              text        NOT NULL,
    original_name     text        NOT NULL,
    original_overview text        NOT NULL DEFAULT '',
    original_language text        NOT NULL DEFAULT '',   -- TheTVDB's three-letter codes, e.g. 'eng'
    original_country  text        NOT NULL DEFAULT '',   -- e.g. 'usa'
    image             text        NOT NULL DEFAULT '',
    status            text        NOT NULL CHECK (status IN ('continuing', 'ended', 'upcoming', 'unknown')),
    first_aired       date,
    last_aired        date,
    next_aired        date,
    airs_time         text        NOT NULL DEFAULT '',   -- local to the series' country, as TheTVDB gives it
    airs_days         text[]      NOT NULL DEFAULT '{}',
    network           text        NOT NULL DEFAULT '',
    average_runtime   integer     NOT NULL DEFAULT 0,    -- minutes; 0 = unknown
    tvdb_updated_at   timestamptz,                       -- TheTVDB's lastUpdated
    synced_at         timestamptz NOT NULL               -- when we last refreshed our copy
);
CREATE INDEX series_synced_at ON series (synced_at);

CREATE TABLE series_translation (
    series_id bigint NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    language  text   NOT NULL CHECK (language IN ('en', 'es')),
    name      text   NOT NULL DEFAULT '',
    overview  text   NOT NULL DEFAULT '',
    PRIMARY KEY (series_id, language)
);

CREATE TABLE genre (
    id   bigint PRIMARY KEY,
    name text   NOT NULL,   -- English, as TheTVDB names them; Spanish names are mapped in code
    slug text   NOT NULL DEFAULT ''
);

CREATE TABLE series_genre (
    series_id bigint NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    genre_id  bigint NOT NULL REFERENCES genre (id),
    PRIMARY KEY (series_id, genre_id)
);

CREATE TABLE season (
    id        bigint  PRIMARY KEY,
    series_id bigint  NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    number    integer NOT NULL,                          -- 0 = specials
    name      text    NOT NULL DEFAULT '',
    image     text    NOT NULL DEFAULT '',
    UNIQUE (series_id, number)
);

-- Upserted by id on every refresh, so anything that later references an episode survives it.
CREATE TABLE episode (
    id                bigint  PRIMARY KEY,
    series_id         bigint  NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    season_number     integer NOT NULL,
    number            integer NOT NULL,
    absolute_number   integer NOT NULL DEFAULT 0,
    original_name     text    NOT NULL DEFAULT '',
    original_overview text    NOT NULL DEFAULT '',
    aired             date,                              -- null while unannounced
    runtime           integer NOT NULL DEFAULT 0,        -- minutes; 0 = unknown
    image             text    NOT NULL DEFAULT '',
    finale_type       text    NOT NULL DEFAULT ''
);
CREATE INDEX episode_series_order ON episode (series_id, season_number, number);
CREATE INDEX episode_aired ON episode (aired) WHERE aired IS NOT NULL;

CREATE TABLE episode_translation (
    episode_id bigint NOT NULL REFERENCES episode (id) ON DELETE CASCADE,
    language   text   NOT NULL CHECK (language IN ('en', 'es')),
    name       text   NOT NULL DEFAULT '',
    overview   text   NOT NULL DEFAULT '',
    PRIMARY KEY (episode_id, language)
);

-- Small key/value state for background jobs, e.g. the tvdb-sync /updates cursor.
CREATE TABLE sync_state (
    key        text        PRIMARY KEY,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE sync_state;
DROP TABLE episode_translation;
DROP TABLE episode;
DROP TABLE season;
DROP TABLE series_genre;
DROP TABLE genre;
DROP TABLE series_translation;
DROP TABLE series;
ALTER TABLE users DROP COLUMN language;
