.PHONY: dev dev-api dev-ui build build-ui build-go gen test lint clean-dev

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
SCENARIO ?= idle

# Dev-only credentials: user "admin", password "devpassword".
DEV_ENV = PANEL_ENV=dev PANEL_DRIVERS=fake PANEL_SCENARIO=$(SCENARIO) \
	PANEL_INSTALL_DIR=$(CURDIR)/.dev/install PANEL_DATA_DIR=$(CURDIR)/.dev/data \
	PANEL_BACKUP_DIR=$(CURDIR)/.dev/backups PANEL_CACHE_DIR=$(CURDIR)/.dev/cache \
	PANEL_DB_PATH=$(CURDIR)/.dev/data/pzman.db PANEL_MODS_TOKEN=dev-token \
	PANEL_ADMIN_PASSWORD_HASH='$$2a$$12$$45hHE890y/mv2tk0eXypde2z/XAtr24o8eHt/SMRdUkZhjoYU5Qs.' \
	PANEL_JWT_SECRET=dev-only-secret SERVER_NAME=devserver USE_STEAM=false

dev:
	$(MAKE) -j2 dev-ui dev-api

# wgo is pinned in go.mod (tool directive): nothing to install.
# Exec the wgo binary directly rather than via `go tool wgo`: `go tool` forwards
# SIGINT to wgo on top of the one the terminal already sends, and wgo treats a
# second SIGINT as a hard interrupt (exits without waiting for pzman), which
# leaves processes running in the background after Ctrl+C.
dev-api:
	@echo "[dev-api] building the Go backend (the first build takes a minute)…"
	$(DEV_ENV) exec "$$(go tool -n wgo)" run ./cmd/pzman

dev-ui:
	cd web && bun run dev

build: build-ui build-go

build-ui:
	cd web && bun run build

build-go:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o pzman ./cmd/pzman

gen:
	go generate ./internal/store
	go run ./cmd/pzman openapi > api/openapi.json
	cd web && bunx openapi-typescript ../api/openapi.json -o src/api/schema.d.ts

test:
	go test ./...
	cd web && bun run test

lint:
	golangci-lint run
	cd web && bunx biome ci .

# Wipes the fake dev tree (switch scenarios cleanly).
clean-dev:
	rm -rf .dev
