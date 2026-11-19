# Contract lives in the sibling Proto repo (CI checks it out next to Backend).
SPEC ?= ../Proto/openapi/v1/streamnext.yaml
ENV  ?= dev
BRU  := npx --yes @usebruno/cli@2.3.0

# StreamNext's own age key. sops only looks at keys.txt (WellSpent's key) by default, so point it
# here for every make target. Override with AGE_KEY_FILE=...; CI (no such file) supplies SOPS_AGE_KEY.
AGE_KEY_FILE ?= $(HOME)/Library/Application Support/sops/age/streamnext.txt
ifeq ($(shell test -f "$(AGE_KEY_FILE)" && echo yes),yes)
export SOPS_AGE_KEY_FILE := $(AGE_KEY_FILE)
endif

# generate = API server (gen/api, gitignored) + sqlc (internal/sqlc, committed).
.PHONY: generate generate-api generate-sql run migrate test bruno db-env db-up db-down secrets-encrypt secrets-decrypt

generate: generate-api generate-sql

generate-api:
	go tool oapi-codegen -config oapi-codegen.yaml $(SPEC)

# needs sqlc on PATH (brew install sqlc); CI runs `sqlc diff` so the committed output can't go stale
generate-sql:
	sqlc generate

run:
	sops exec-env $(ENV).enc.env 'go run ./cmd/server'

migrate:
	sops exec-env $(ENV).enc.env 'go run ./cmd/migrate'

# Unit + testcontainers tests with a 90% coverage gate on internal/, excluding generated sqlc code and the testdb helper.
test: generate-api
	go vet ./...
	go test ./internal/... -covermode=atomic -coverprofile=cover.out
	@grep -v -e '/internal/sqlc/' -e '/internal/testdb/' cover.out > cover.gate.out
	@go tool cover -func=cover.gate.out | awk '/^total:/ {gsub("%","",$$3); print "coverage: " $$3 "%"; if ($$3+0 < 90) {print "FAIL: below 90%"; exit 1}}'

# End-to-end: Postgres + migrate + API in Docker, then the Bruno collection.
bruno: generate-api
	docker compose up -d --build
	until curl -fs localhost:8080/v1/health >/dev/null; do sleep 1; done
	cd bruno && $(BRU) run --env ci; s=$$?; cd .. && docker compose down -v; exit $$s

# Standalone dev database (docker-compose.db.yml). Run on the DB host; see that file's header.
db-env:
	sops exec-env $(ENV).enc.env 'printf "POSTGRES_PASSWORD=%s\n" "$$POSTGRES_PASSWORD" > .env.db'

db-up: db-env
	docker compose -f docker-compose.db.yml --env-file .env.db up -d --wait

db-down:
	docker compose -f docker-compose.db.yml --env-file .env.db down

secrets-decrypt:
	sops decrypt --output $(ENV).env $(ENV).enc.env

secrets-encrypt:
	sops encrypt --filename-override $(ENV).enc.env --output $(ENV).enc.env $(ENV).env
