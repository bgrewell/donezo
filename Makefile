# donezo monorepo Makefile
#
# Dev notes:
#   - Frontend dev server runs as the donezo-dev systemd user service
#     (hot reload on :5173); `make build` is for production bundles only.
#   - Backend dev loop: `go run ./cmd/donezod --data-dir /tmp/donezo-dev \
#     --seed seed/seed.json --port 8787`. --seed is a no-op on an
#     already-seeded dir (safe to leave set); use a fresh dir to reseed.
#   - Dev builds serve the API only; `make release-build` produces the
#     single-file release binary with the web bundle embedded via
#     go:embed behind the embedui build tag.

# Release version: env VERSION wins, then git describe, then a dev stamp.
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BRANCH      := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)

LDFLAGS := -X 'main.appVersion=$(VERSION)' \
           -X 'main.appBuildDate=$(BUILD_DATE)' \
           -X 'main.appCommitHash=$(COMMIT_HASH)' \
           -X 'main.appBranch=$(BRANCH)'

.PHONY: build release-build test lint seed-json clean dev-upgrade dev-snapshots

## build: production web bundle + donezod binary at bin/donezod
build:
	npm --prefix web run build
	go build -ldflags="$(LDFLAGS)" -o bin/donezod ./cmd/donezod

## release-build: single-file release binary at bin/donezod-release with
## the web bundle embedded (-tags embedui). web/dist is staged into
## internal/webui/dist only for the compile and removed afterwards (the
## trap cleans up even when the build fails). Cross-compile via env,
## e.g.: CGO_ENABLED=0 GOOS=linux GOARCH=arm64 make release-build
## (modernc.org/sqlite is pure Go, so CGO is never needed).
release-build:
	npm --prefix web run build
	rm -rf internal/webui/dist
	cp -r web/dist internal/webui/dist
	@trap 'rm -rf internal/webui/dist' EXIT; \
	go build -tags embedui -ldflags="$(LDFLAGS)" -o bin/donezod-release ./cmd/donezod

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

## dev-upgrade: rebuild donezod and roll the dev service (auto-snapshots
## the data dir via the unit's ExecStartPre before the new binary starts)
dev-upgrade:
	go build -ldflags="$(LDFLAGS)" -o bin/donezod ./cmd/donezod
	systemctl --user restart donezod-dev
	@sleep 1.5
	@curl -sf http://localhost:8787/api/healthz >/dev/null && echo "donezod-dev upgraded and healthy" || (echo "donezod-dev unhealthy after upgrade — check: journalctl --user -u donezod-dev"; exit 1)

## dev-snapshots: list dev data snapshots (restore with scripts/dev-restore.sh <ts>)
dev-snapshots:
	@./scripts/dev-restore.sh

## clean: remove build outputs
clean:
	rm -rf bin web/dist
