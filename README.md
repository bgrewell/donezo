# donezo

Personal work memory and attention system. Captures tasks, reminders, notes,
and completed work with minimal friction; recovers context after
interruptions; shows where effort actually went on a project-by-time
timeline (**Project Pulse**).

Frontend prototype: React 18 + TypeScript + Vite + Tailwind CSS, built on the
[@grewelltech/console](https://github.com/grewelltech/design-system) design
system. Mock data only — no backend, auth, or persistence yet.

## Development

The dev server runs as a **systemd user service** with hot reload, so the app
is always live at <http://localhost:5173>:

```sh
systemctl --user status donezo-dev     # health
journalctl --user -u donezo-dev -f     # logs
systemctl --user restart donezo-dev    # bounce after dependency changes
```

The unit file lives at `deploy/donezo-dev.service` (installed to
`~/.config/systemd/user/`). Manual alternative: `npm run dev`.

Other scripts:

```sh
npm run typecheck        # tsc --noEmit
npm run build            # typecheck + production build
npm run peek -- "#/timeline" .peek/t.png [--theme slate] [--full]
                         # Playwright screenshot of the running app
```

## Design system

GTech Console is consumed as a source `file:` dependency from `../design-system`
(clone of grewelltech/design-system). Its tokens (`--gtc-*`), Tailwind preset,
and components are the base layer; donezo adds `--dz-*` tokens (project color
ramp, shell metrics) in `src/styles/app.css`.

Improvements made here that generalize (new tokens, component tweaks) should
be committed back to the design-system repo to keep it current.

### Theming

Every component references CSS variables only. A theme is a set of variable
overrides keyed by `data-theme` on `<html>` (see `src/styles/themes.css`),
applied by `ThemeProvider` and persisted to localStorage. Built-in: `console`
(default) and `slate`. Adding a theme = adding a CSS block + one registry
entry in `src/lib/themes.ts`.

## Structure

```
src/
  domain/      types + mock dataset (the only place data lives)
  state/       AppStore (reducer + actions), selectors, ThemeProvider
  lib/         time math (date-fns), ids, project colors, theme registry
  components/
    shell/     nav rail, top bar, inspector, quick capture, app shell
    ui/        Radix primitives styled to GTech Console (tooltip, popover, menu)
    common/    project marks, status badges, activity-type metadata
  views/       one directory per primary view (Timeline is Project Pulse)
```

Views are hash-routed (`#/timeline`, `#/projects/loom`, …) so they are
bookmarkable and screenshot-addressable.
