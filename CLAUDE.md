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
| `make run` / `make migrate` | run against `ENV=dev` secrets (sops) |
| `make secrets-decrypt` / `secrets-encrypt` | edit `<env>.env` (gitignored), re-encrypt to `<env>.enc.env` |

## Secrets (sops + age)
Encrypted files are `dev.enc.env` / `prod.enc.env` (the `.env` suffix is needed for sops to detect the format). The StreamNext age key is `~/Library/Application Support/sops/age/streamnext.txt` — **not** WellSpent's `keys.txt`. Export `SOPS_AGE_KEY_FILE` to that path. CI uses the `SOPS_AGE_KEY` secret.

## Layer order for a feature
Contract (Proto) → `make generate` → migration + sqlc query → repository (interface) → service (unit tests with mocked repos) → REST handler (httptest) → Bruno requests. `sqlc.yaml` is configured but `sqlc generate` needs at least one query; add it with the first real query (M1) and wire it into `make generate`.

## Rules
- Errors: return `*apperr.Error`; `rest.writeError` is the only place that maps them to HTTP. Untyped errors become a generic 500 — never leak internals.
- Every process sets its own `application_name`. Neon: **direct** URL, not the pooler.
- Log with IDs via zap; never swallow an error.
- Tests must assert behaviour, not just compile. Keep `cmd/` thin (it is outside the coverage gate).

## Versioning & git
Bump `internal/version/version.go` and add a `CHANGELOG.md` entry with every feature (semver). Work on `develop`, PR to `main`, auto-merge when CI is green. **No AI attribution trailers in commits.**
