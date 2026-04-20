GOOSE_DRIVER := postgres
GOOSE_DBSTRING ?= $(DATABASE_URL)
GOOSE_MIGRATION_DIR := sql/schema

.PHONY: up down api worker migrate migrate-down sqlc build lint test integration-test tidy check-migrations ci-check

## Infrastructure
up:
	docker compose up -d

down:
	docker compose down

## Run services locally
api:
	go run ./api/...

worker:
	go run ./worker/...

## Database migrations (requires goose: go install github.com/pressly/goose/v3/cmd/goose@latest)
migrate:
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" \
		goose -dir $(GOOSE_MIGRATION_DIR) up

migrate-down:
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" \
		goose -dir $(GOOSE_MIGRATION_DIR) down

migrate-status:
	GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" \
		goose -dir $(GOOSE_MIGRATION_DIR) status

## Code generation
sqlc:
	sqlc generate

## Build binaries
build:
	go build -o bin/api ./api/...
	go build -o bin/worker ./worker/...

## Quality
lint:
	go vet ./...

test:
	go test ./... -race -count=1

integration-test:
	docker compose up -d postgres rabbitmq redis
	go test -tags=integration ./integration/... -count=1 -v

tidy:
	go mod tidy

check-migrations:
	bash ./scripts/check-migrations.sh

ci-check: lint test sqlc check-migrations
