# Changelog

Semver. Minor = new feature/endpoint, patch = fix, major = breaking contract change.
The version is `internal/version/version.go`. Backend and Proto versions are independent; an entry says when it implements a new Proto version.

## 0.5.0 — 2026-11-19
Implements Proto 0.5.0 (catalog, M2).
- `GET /v1/search/series`, `GET /v1/series/{seriesId}`, `GET /v1/series/{seriesId}/episodes`; every catalog endpoint needs a sign-in. Names and overviews come in the user's profile language (`en` or `es`, new `language` on `GET`/`PATCH /v1/me`), falling back to English and then the original language; genres are named in Spanish from a built-in map.
- A series is fetched from TheTVDB the first time someone opens it and stored in Postgres; a stored series is never refreshed by the API. Search is a pass-through with a 10-minute per-instance cache.
- `cmd/jobs/tvdb-sync` (`make tvdb-sync`) keeps stored series fresh from TheTVDB's `/updates` feed (series records only, which the real API shows covers episode changes), with a cursor that only advances after a clean run, a full refresh when the cursor is missing or older than 80 days, deletion handling, and a 30-day safety net.
- Episodes are upserted by their TheTVDB id and only vanished ones are removed, so later references (watch history) survive refreshes.
- `internal/tvdb`: the TheTVDB client (login with token renewal, one retry on 401, zero-based paging, alias-aware translations). `cmd/tvdbrecord` (`make tvdb-record`) records trimmed real fixtures with overviews replaced by placeholders; `cmd/tvdbstub` replays them in the CI stack, so Bruno runs the catalog with no API key.
- Migration `00005_catalog` (`series`, `series_translation`, `genre`, `series_genre`, `season`, `episode`, `episode_translation`, `sync_state`, `users.language`).
- New config: `TVDB_API_KEY`, `TVDB_BASE_URL` (optional). Without a key the catalog answers 503. Bruno: catalog flow (suite: 62 requests, 151 assertions).

## 0.4.0 — 2026-11-19
Implements Proto 0.4.0 (social sign-in and devices, M1 slice 3).
- `POST /v1/auth/apple` and `/v1/auth/google`. `internal/social` verifies the ID token (RS256 only, issuer, audience, expiry, Apple nonce as SHA-256) against the provider's keys, cached for 1h, refetched once on an unknown key id (at most once a minute) and kept through a short provider outage.
- Account rules: a known identity signs in its user; a provider-verified email links to the existing account (**clearing the password and revoking the sessions of an account whose email was never verified**, so a pre-registered attacker is locked out); an unverified provider email never links (409); a new account is created otherwise. Providers always get 90-day device-bound sessions.
- `PUT`/`DELETE /v1/devices/{deviceId}`: push (FCM) tokens, one owner per token (registering it moves it); concurrent registrations are retried. The first route with a path parameter.
- `forgot-password` now also works for accounts without a password, so a social-only user can set one.
- User creation takes a `NewUser` struct. Migration `00004` (`oauth_account`, `device`).
- New config: `APPLE_CLIENT_ID`, `GOOGLE_CLIENT_IDS` (a provider without client ids answers 503). Strict rate-limit burst is now 40.
- Bruno: devices and social flows (suite: 45 requests, 97 assertions).

## 0.3.0 — 2026-11-19
Implements Proto 0.3.0 (auth email flows and account, M1 slice 2).
- `POST /v1/auth/{verify-email,resend-verification,forgot-password,reset-password}`, `POST /v1/me/change-password`, `DELETE /v1/me`.
- Register sends a verification email (a mail failure never blocks sign-up). Single-use email tokens, stored as SHA-256 hashes: 24h to verify, 1h to reset; a newer token supersedes older ones; one email per address per purpose per 60 seconds.
- Resetting or changing the password revokes every session; change-password returns a new session for the requesting device.
- **Behaviour change:** refresh-token reuse detection now applies only to tokens that were *rotated*. A token revoked by logout, a newer login or a password change/reset is just a 401 and no longer signs the user out everywhere (it would have undone a fresh login after a password reset).
- `internal/mail`: log transport (dev only, refused when `ENV=prod`) and a Resend transport over plain `net/http`.
- New config: `ENV`, `APP_BASE_URL`, `MAIL_PROVIDER`, `MAIL_FROM`, `RESEND_API_KEY` (all optional in dev).
- Strict rate limit now also covers `POST /v1/me/change-password` and `DELETE /v1/me`; defaults are global 10/s burst 60, strict 0.5/s burst 30.
- Migration `00003_email_token`. Bruno: 14 account-flow requests (suite: 34 requests, 78 assertions).

## 0.2.0 — 2026-11-19
Implements Proto 0.2.0 (auth core, M1 slice 1).
- `POST /v1/auth/{register,login,refresh,logout}` and `GET`/`PATCH /v1/me`.
- argon2id passwords, 15-minute HS256 access tokens, opaque device-bound refresh tokens stored as SHA-256 hashes, rotation on every refresh, reuse detection that revokes all sessions, and a 10s grace window for lost responses (`REFRESH_GRACE_SECONDS`).
- Bearer protection is driven by the contract's `security` (embedded spec), not hand-written route lists.
- Per-IP rate limiting (global, plus stricter on `/v1/auth/`), 429 with `Retry-After`.
- Migration `00002_auth` (`users`, `refresh_token`); first sqlc queries (generated code is committed).
- New required config: `JWT_SECRET` (at least 32 bytes). Bruno gains a 17-request auth flow.

## 0.1.4 — 2026-11-19
- `scripts/open-db-firewall.ps1`: opens inbound TCP 5433 (Private networks, local subnet) on a Windows DB host.

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
