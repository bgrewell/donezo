# Contributing to donezo

Thanks for poking around. This covers running donezo from source, the
monorepo layout, and where things live. For the HTTP/MCP wire contract
see [`docs/api.md`](docs/api.md); for known issues and planned work see
[`docs/backlog.md`](docs/backlog.md).

## Monorepo layout

```
cmd/donezod/     backend entry point
internal/
  api/           HTTP layer (stdlib net/http, Go 1.22 routing)
  auth/          argon2id passwords, session tokens/authenticator, rate limiter, sweeper
  config/        flags/env configuration
  mcp/           the /mcp endpoint (thin wrapper over the official MCP Go SDK)
  seed/          seed.json import
  store/         core store (users/sessions/spaces) + space store (per-space SQLite)
seed/            committed seed.json (regenerate: make seed-json)
web/
  src/
    domain/      types + mock dataset (the only place data lives)
    state/       AppStore (reducer + actions), selectors, ThemeProvider
    lib/         time math (date-fns), ids, project colors, theme registry
    components/
      shell/     nav rail, top bar, inspector, quick capture, app shell
      ui/        Radix primitives styled to GTech Console (tooltip, popover, menu)
      common/    project marks, status badges, activity-type metadata
    views/       one directory per primary view (Timeline is Project Pulse)
  scripts/       peek.mjs (screenshots), export-seed.mjs (seed.json generator)
```

Views are hash-routed (`#/timeline`, `#/projects/loom`, …) so they are
bookmarkable and screenshot-addressable.

## Frontend (`web/`)

The dev server runs as a **systemd user service** with hot reload, so the
app is always live at <http://localhost:5173>:

```sh
systemctl --user status donezo-dev     # health
journalctl --user -u donezo-dev -f     # logs
systemctl --user restart donezo-dev    # bounce after dependency changes
```

The unit file lives at `deploy/donezo-dev.service` (installed to
`~/.config/systemd/user/`). Manual alternative: `cd web && npm run dev`.

Other scripts (run inside `web/`):

```sh
npm run typecheck        # tsc --noEmit
npm run build             # typecheck + production build
npm run peek -- "#/timeline" .peek/t.png [--theme slate] [--full]
                          # Playwright screenshot of the running app
```

## Backend (donezod)

```sh
make build               # build web + bin/donezod
make test                 # go test ./...
make lint                 # gofmt + golangci-lint + go vet
make seed-json             # regenerate seed/seed.json from web mock data
bin/donezod --port 8787 --data-dir ~/.local/share/donezo --seed seed/seed.json
```

A systemd user unit for the backend dev loop lives at
`deploy/donezod-dev.service` (runs `bin/donezod` on port 8787 with data
under `~/.local/share/donezo-dev`; install to `~/.config/systemd/user/`
like `donezo-dev.service`).

Flags (each also has a `DONEZOD_*` env fallback): `--port`, `--data-dir`,
`--seed`, `--trust-proxy` (trust proxy headers: the **last**
`X-Forwarded-For` hop — the one the proxy itself appended — keys rate
limiting, and `X-Forwarded-Proto: https` marks session cookies `Secure`;
also governs the MCP transport's DNS-rebinding guard, see
[`docs/api.md`](docs/api.md#mcp-mcp); set it only when donezod sits
directly behind a reverse proxy, since without one those headers are
attacker-controlled and are ignored), and `--dev-auto-login` (see
[`docs/api.md`](docs/api.md#security-posture)).

## Design system

GTech Console is consumed as a source `file:` dependency on `../design-system`
(the sibling clone of grewelltech/design-system, relative to the repo root —
declared as `file:../../design-system` in `web/package.json`, where `file:`
paths resolve relative to `web/`). Its tokens (`--gtc-*`), Tailwind preset,
and components are the base layer; donezo adds `--dz-*` tokens (project color
ramp, shell metrics) in `src/styles/app.css`.

Improvements made here that generalize (new tokens, component tweaks) should
be committed back to the design-system repo to keep it current.

### Theming

Every component references CSS variables only. A theme is a set of variable
overrides keyed by `data-theme` on `<html>` (see `src/styles/themes.css`),
applied by `ThemeProvider` and persisted to localStorage. Built-in themes:
`console` (default), `slate`, `paper`, and `blossom`. The Appearance menu
also carries two more axes, applied the same way: font set (`plex` default,
`inter`, `system`) and text size (`small`, `medium` default, `large`).
Adding a theme = adding a CSS block + one registry entry in
`src/lib/themes.ts`.

## Before opening a PR

- `make lint && make test` (Go) and `npm run typecheck` (frontend) clean.
- New or changed Go behavior gets table-driven tests (happy/error/boundary)
  and GoDoc on exported symbols.
- If you touched the API surface (REST or MCP), update
  [`docs/api.md`](docs/api.md) in the same change.
