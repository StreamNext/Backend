# Changelog

Semver. Minor = new feature/endpoint, patch = fix, major = breaking contract change.
The version is `internal/version/version.go`. Backend and Proto versions are independent; an entry says when it implements a new Proto version.

## 0.1.1 — 2026-11-19
- Unknown routes and wrong methods now return the JSON `{code,message}` error (404 `not_found` / 405 `method_not_allowed` with `Allow`) instead of Go's plain text.
- Split `internal/rest` into one file per concern (no behaviour change).

## 0.1.0 — 2026-11-19
- Foundation (M0): config, Postgres pool (`application_name` per process), embedded goose migrations, typed errors with lowercase `{code,message}` bodies, `GET /v1/health`.
- Unit tests, testcontainers Postgres tests, 90% coverage gate, Bruno integration suite, Docker/compose, CI and Cloud Run deploy workflows.
