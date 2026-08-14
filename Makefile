.PHONY: dev dev-memory ocr-build test fmt migrate

GOCACHE ?= $(CURDIR)/.cache/gocache
GOMODCACHE ?= $(CURDIR)/.cache/gomod
export GOCACHE GOMODCACHE GOSUMDB=off

dev:
	@test -f .env || cp .env.example .env
	@set -a && . ./.env && set +a && go run ./cmd/api

dev-memory:
	DATABASE_DSN=memory go run ./cmd/api

ocr-build:
	@mkdir -p .cache/bin
	swiftc tools/ocr_vision.swift -o .cache/bin/ocr_vision

test:
	go test ./...

fmt:
	gofmt -w cmd internal tools

migrate:
	@test -f .env || cp .env.example .env
	@set -a && . ./.env && set +a && go run ./cmd/migrate
