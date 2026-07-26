# donezo monorepo Makefile
#
# Dev notes:
#   - Frontend dev server runs as the donezo-dev systemd user service
#     (hot reload on :5173); `make build` is for production bundles only.
#   - Backend dev loop: `go run ./cmd/donezod --data-dir /tmp/donezo-dev \
#     --seed seed/seed.json --port 8787`. --seed is a no-op on an
#     already-seeded dir (safe to leave set); use a fresh dir to reseed.
#   - Phase 1 serves the API only; the web bundle is NOT embedded yet
#     (planned for phase 3 via go:embed of web/dist).

VERSION     ?= 0.1.0-dev
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BRANCH      := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)

LDFLAGS := -X 'main.appVersion=$(VERSION)' \
           -X 'main.appBuildDate=$(BUILD_DATE)' \
           -X 'main.appCommitHash=$(COMMIT_HASH)' \
           -X 'main.appBranch=$(BRANCH)'

.PHONY: build test lint seed-json clean

## build: production web bundle + donezod binary at bin/donezod
build:
	npm --prefix web run build
	go build -ldflags="$(LDFLAGS)" -o bin/donezod ./cmd/donezod

## test: run all Go tests
test:
	go test ./...

## lint: gofmt check, golangci-lint (if installed), go vet
lint:
	@fmt_out=$$(gofmt -l .); if [ -n "$$fmt_out" ]; then echo "gofmt needed:"; echo "$$fmt_out"; exit 1; fi
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed; skipping"; fi
	go vet ./...

## seed-json: regenerate seed/seed.json from the frontend mock dataset
seed-json:
	cd web && node --experimental-strip-types scripts/export-seed.mjs

## clean: remove build outputs
clean:
	rm -rf bin web/dist
