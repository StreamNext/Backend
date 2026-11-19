# Changelog

Semver. Minor = new feature/endpoint, patch = fix, major = breaking contract change.
The version is `internal/version/version.go`. Backend and Proto versions are independent; an entry says when it implements a new Proto version.

## 0.1.3 — 2026-11-19
- Standalone dev database: `docker-compose.db.yml` (own project, volume and port 5433), `make db-env/db-up/db-down`, `.env.db.example`. `dev.enc.env` now holds `POSTGRES_PASSWORD` and the real dev `DATABASE_URL`.

## 0.1.2 — 2026-11-19
- Makefile points sops at StreamNext's own age key automatically (`AGE_KEY_FILE`), so `make secrets-*`, `run` and `migrate` work in a fresh shell.
- `secrets-decrypt`/`encrypt` write with `--output`, so a failed run can no longer truncate an existing `dev.env`.

## 0.1.1 — 2026-11-19
- Unknown routes and wrong methods now return the JSON `{code,message}` error (404 `not_found` / 405 `method_not_allowed` with `Allow`) instead of Go's plain text.
- Split `internal/rest` into one file per concern (no behaviour change).

## 0.1.0 — 2026-11-19
- Foundation (M0): config, Postgres pool (`application_name` per process), embedded goose migrations, typed errors with lowercase `{code,message}` bodies, `GET /v1/health`.
- Unit tests, testcontainers Postgres tests, 90% coverage gate, Bruno integration suite, Docker/compose, CI and Cloud Run deploy workflows.
