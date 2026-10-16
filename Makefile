GOOSE   := go run github.com/pressly/goose/v3/cmd/goose@v3.29.0
SQLC    := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
LINT    := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

-include .env
export

# Raw market data folder (not abspath: CURDIR contains spaces). Override with DATA_DIR=...
DATA_DIR ?= $(CURDIR)/../data/assets&news

.PHONY: run build test lint fmt tidy migrate-up migrate-down migrate-new sqlc check import-market classify-assets

run:
	go run ./cmd/api

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api

test:
	go test -race -count=1 ./...

lint:
	$(LINT) run

fmt:
	gofmt -w .

tidy:
	go mod tidy

migrate-up:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_URL)" down

migrate-new:
	$(GOOSE) -dir db/migrations create $(name) sql

sqlc:
	$(SQLC) generate

check: fmt lint test

import-market:
	DATA_DIR="$(DATA_DIR)" go run ./cmd/importer $(IMPORT_ARGS)

classify-assets:
	go run ./cmd/classifier $(CLASSIFY_ARGS)
