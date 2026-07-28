# Backlog

Known issues and deferred work, in rough priority order. Prototype-phase
items only — backend/persistence work tracks separately once it starts.

## Bugs (deferred, low)

- **Appearance menu goes soft after changing text size.** The open Radix
  dropdown is positioned with a pixel-computed `translate3d`; a root
  font-size change makes its rem-based layout fractional and the layer
  rasters off the pixel grid until closed/reopened. Fix candidates: re-key
  `DropdownMenuContent` on `fontSize` (forces remount), or round the
  popper transform. One-liner either way. (Reported by Ben 2026-07-25.)

## Next feature

- **Action queues** (Ben, 2026-07-27): evolve next-action beyond text —
  cheap, interrelated, project-tied action chains where completing one
  auto-surfaces the next in line, instead of the user re-deciding from
  the task list each time. The alternates-promotion flow (shipping in the
  project editing & lifecycle pass) is the v1; queues are v2.

## Upstream candidates (design-system / @grewelltech/console)

- Tailwind preset colors lack `<alpha-value>` support, so `/N` opacity
  classes silently no-op (root cause of the invisible dialog scrim).
- Preset `fontFamily` hardcodes Plex stacks instead of referencing
  `--gtc-font-*` vars (donezo overrides locally for font sets).
- `bg-gtc-sheen` gradient and Tabs/Navbar active text-shadows hardcode
  cyan rgba — invisible/wrong on light themes.
- Dialog: background not aria-hidden/inert for screen readers while open.
- Formalize gold for "key temporal readouts" (timeline range label) as an
  official token use, not an exception.

## Accepted prototype limitations

- No JS test runner; pure derivation functions are exported test-ready.
- Tour steps with absent targets (empty inbox) fall back to a centered
  card rather than skipping.
- Focus restore after delete-confirm falls back to body when the opener
  element no longer exists.
- Quarter-zoom prev/next barely move on ultrawide viewports (whole range
  fits on screen).
