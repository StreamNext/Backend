# Changelog

Semver. Minor = new feature/endpoint, patch = fix, major = breaking contract change.
The version is `internal/version/version.go` and must match the Proto `info.version` it implements.

## 0.1.0 — 2026-11-19
- Foundation (M0): config, Postgres pool (`application_name` per process), embedded goose migrations, typed errors with lowercase `{code,message}` bodies, `GET /v1/health`.
- Unit tests, testcontainers Postgres tests, 90% coverage gate, Bruno integration suite, Docker/compose, CI and Cloud Run deploy workflows.
