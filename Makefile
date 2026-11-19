# Contract lives in the sibling Proto repo (CI checks it out next to Backend).
SPEC ?= ../Proto/openapi/v1/streamnext.yaml
ENV  ?= dev
BRU  := npx --yes @usebruno/cli@2.3.0

.PHONY: generate run migrate test bruno secrets-encrypt secrets-decrypt

generate:
	go tool oapi-codegen -config oapi-codegen.yaml $(SPEC)

# sops reads the age key from SOPS_AGE_KEY_FILE (see CLAUDE.md).
run:
	sops exec-env $(ENV).enc.env 'go run ./cmd/server'

migrate:
	sops exec-env $(ENV).enc.env 'go run ./cmd/migrate'

# Unit + testcontainers tests with a 90% coverage gate on internal/.
test: generate
	go vet ./...
	go test ./internal/... -covermode=atomic -coverprofile=cover.out
	@go tool cover -func=cover.out | awk '/^total:/ {gsub("%","",$$3); print "coverage: " $$3 "%"; if ($$3+0 < 90) {print "FAIL: below 90%"; exit 1}}'

# End-to-end: Postgres + migrate + API in Docker, then the Bruno collection.
bruno: generate
	docker compose up -d --build
	until curl -fs localhost:8080/v1/health >/dev/null; do sleep 1; done
	cd bruno && $(BRU) run --env ci; s=$$?; cd .. && docker compose down -v; exit $$s

secrets-decrypt:
	sops decrypt $(ENV).enc.env > $(ENV).env

secrets-encrypt:
	sops encrypt --filename-override $(ENV).enc.env $(ENV).env > $(ENV).enc.env.tmp && mv $(ENV).enc.env.tmp $(ENV).enc.env
