# CLAUDE.md — Backend

Go REST + JWT API for StreamNext. Read `../CLAUDE.md` and `../docs/specs/02-architecture.md` first. Contract: `../Proto/openapi/v1/streamnext.yaml` (hand-written).

## Layout
`cmd/{server,migrate}` · `internal/{apperr,config,db,rest,version}` · `gen/` (gitignored, from `make generate`) · `bruno/` · `internal/db/{migrations,query}`

## Commands
| | |
|---|---|
| `make generate` | oapi-codegen from `../Proto` into `gen/api` (run after any contract change and on a fresh clone) |
| `make test` | vet + unit + testcontainers tests (needs Docker) + **90% coverage gate** on `internal/` |
| `make bruno` | Postgres + migrate + API in Docker, then `bru run --env ci` |
| `make db-up` / `db-down` | start/stop the standalone dev database (run on the DB host) |
| `make run` / `make migrate` | run against `ENV=dev` secrets (sops); needs the dev database up |
| `make secrets-decrypt` / `secrets-encrypt` | edit `<env>.env` (gitignored), re-encrypt to `<env>.enc.env` |

## Dev database
`docker-compose.db.yml` defines a standalone StreamNext Postgres 16 (user `streamnext`, db `streamnext_dev`, port **5433**, own volume), separate from WellSpent's. Start it once on the DB host with `make db-up` (or copy `.env.db.example` to `.env.db`) and leave it running; every machine's `dev.enc.env` points `DATABASE_URL` at that host. Recreate from scratch: `docker compose -f docker-compose.db.yml --env-file .env.db down -v`, then `make db-up` and `make migrate`. Tests never use it: `make test` and `make bruno` start their own throwaway containers.

## Secrets (sops + age)
Encrypted files are `dev.enc.env` / `prod.enc.env` (the `.env` suffix is needed for sops to detect the format). The StreamNext age key is `~/Library/Application Support/sops/age/streamnext.txt` — **not** WellSpent's `keys.txt`. The Makefile points sops at it automatically (override with `AGE_KEY_FILE=...`). When you run `sops` directly, export `SOPS_AGE_KEY_FILE` to that path first, otherwise sops tries WellSpent's `keys.txt` and fails with "no master key was able to decrypt". CI uses the `SOPS_AGE_KEY` secret.

## Layer order for a feature
Contract (Proto) → `make generate` → migration + sqlc query → repository (interface) → service (unit tests with mocked repos) → REST handler (httptest) → Bruno requests. `sqlc.yaml` is configured but `sqlc generate` needs at least one query; add it with the first real query (M1) and wire it into `make generate`.

## Rules
- Errors: return `*apperr.Error`; `rest.writeError` is the only place that maps them to HTTP. Untyped errors become a generic 500 — never leak internals.
- Every process sets its own `application_name`. Neon: **direct** URL, not the pooler.
- Log with IDs via zap; never swallow an error.
- Readability first: one concern per file, named for what it holds (`errors.go`, `middleware.go`, `auth.go`; never `utils.go`). Split a file when it mixes concerns or stops fitting in your head (roughly 200–400 lines). `*rest.Server` methods are spread across one file per domain. Add a package only for a real boundary (e.g. `tvdb`, `service`), not per feature.
- Comment the why, not the what. When using a Go idiom that isn't obvious (implicit interfaces, embedding, generated wiring), say so briefly in the PR/summary: the maintainer is still learning Go.
- Tests must assert behaviour, not just compile. Keep `cmd/` thin (it is outside the coverage gate).

## Versioning & git
Bump `internal/version/version.go` and add a `CHANGELOG.md` entry with every feature (semver). Work on `develop`, PR to `main`, auto-merge when CI is green. **No AI attribution trailers in commits.**
