# donezo

Personal work memory and attention system. Captures tasks, reminders, notes,
and completed work with minimal friction; recovers context after
interruptions; shows where effort actually went on a project-by-time
timeline (**Project Pulse**).

Monorepo:

- `web/` — frontend: React 18 + TypeScript + Vite + Tailwind CSS, built on the
  [@grewelltech/console](https://github.com/grewelltech/design-system) design
  system.
- Go backend **donezod** (`cmd/donezod`, `internal/`) — SQLite-backed API
  server. One SQLite file per space; `core.db` holds the users/spaces
  registry. Auth is stubbed (phase 2).

## Development

### Frontend (`web/`)

The dev server runs as a **systemd user service** with hot reload, so the app
is always live at <http://localhost:5173>:

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
npm run build            # typecheck + production build
npm run peek -- "#/timeline" .peek/t.png [--theme slate] [--full]
                         # Playwright screenshot of the running app
```

### Backend (donezod)

```sh
make build               # build web + bin/donezod
make test                # go test ./...
make lint                # gofmt + golangci-lint + go vet
make seed-json           # regenerate seed/seed.json from web mock data
bin/donezod --port 8787 --data-dir ~/.local/share/donezo --seed seed/seed.json
```

API surface (phase 1): `GET /api/healthz`, `GET /api/spaces`,
`GET /api/spaces/{id}/state`.

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

## Structure

```
cmd/donezod/     backend entry point
internal/
  api/           HTTP layer (stdlib net/http, Go 1.22 routing)
  config/        flags/env configuration
  seed/          seed.json import
  store/         core store (users/spaces) + space store (per-space SQLite)
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
