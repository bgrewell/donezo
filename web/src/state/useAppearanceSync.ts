import * as React from "react";

import { fetchUserSettings, saveUserSettings, type UserSettings } from "@/api/client";
import {
  FONT_SETS,
  FONT_SIZES,
  THEMES,
  type FontSetId,
  type FontSizeId,
  type ThemeId,
} from "@/lib/themes";
import { useTheme } from "@/state/ThemeProvider";

// Appearance preferences are stored on the server so they follow the user
// between machines, but localStorage stays the source of truth at first
// paint: ThemeProvider reads it synchronously before anything renders, so
// there is no flash of the default theme while a fetch is in flight. This
// hook reconciles the two — hydrate once from the server, then write through
// on every later change.
//
// It must be mounted inside the authenticated tree; an anonymous caller has
// no settings to read and every request would 401.

/** The three appearance preferences, as stored. */
interface Appearance {
  theme: string;
  font: string;
  fontSize: string;
}

/** Narrows a stored string to a known id, ignoring values this build does
 *  not recognize (a preference saved by a newer version, say). */
function known<T extends string>(
  value: string | undefined,
  options: readonly { id: T }[]
): T | null {
  if (!value) return null;
  return options.some((o) => o.id === value) ? (value as T) : null;
}

/** Keeps the user's appearance preferences in sync with the server.
 *
 *  Both directions are best-effort: a failed read leaves the local
 *  preferences untouched, and a failed write is dropped rather than
 *  surfaced. Appearance is not worth interrupting someone over, and
 *  localStorage has already persisted the change locally either way. */
export function useAppearanceSync(): void {
  const { theme, setTheme, font, setFont, fontSize, setFontSize } = useTheme();

  // The live values, readable from an effect that must not re-run when they
  // change. Without this the hydrate effect would close over its mount-time
  // values and seed a baseline that is already out of date if the user
  // changed something while the first read was in flight.
  const latest = React.useRef<Appearance>({ theme, font, fontSize });
  latest.current = { theme, font, fontSize };

  // What the server is believed to hold. Null until the first read settles,
  // which is what keeps the hydrating write from echoing straight back.
  const remote = React.useRef<Appearance | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    fetchUserSettings()
      .then((settings) => {
        if (cancelled) return;
        const storedTheme = known(settings.theme, THEMES);
        const storedFont = known(settings.font, FONT_SETS);
        const storedFontSize = known(settings.fontSize, FONT_SIZES);
        if (storedTheme) setTheme(storedTheme as ThemeId);
        if (storedFont) setFont(storedFont as FontSetId);
        if (storedFontSize) setFontSize(storedFontSize as FontSizeId);
        // Record what the server now agrees with, so the state updates above
        // are not mistaken for a local edit and pushed straight back.
        const now = latest.current;
        remote.current = {
          theme: storedTheme ?? now.theme,
          font: storedFont ?? now.font,
          fontSize: storedFontSize ?? now.fontSize,
        };
      })
      .catch(() => {
        if (cancelled) return;
        // Could not read (offline, or the session lapsed). Treat the current
        // local state as the baseline so a later deliberate change still
        // gets a chance to save.
        remote.current = { ...latest.current };
      });
    return () => {
      cancelled = true;
    };
    // Runs once per mount; live values are read through the ref above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  React.useEffect(() => {
    const prev = remote.current;
    if (prev === null) return; // not hydrated yet
    // Send only what actually changed. Writing all three would let a tab
    // that has been open a while overwrite a preference changed on another
    // device with its own stale copy, turning an unrelated edit into a
    // silent revert.
    const patch: UserSettings = {};
    if (theme !== prev.theme) patch.theme = theme;
    if (font !== prev.font) patch.font = font;
    if (fontSize !== prev.fontSize) patch.fontSize = fontSize;
    if (Object.keys(patch).length === 0) return;

    remote.current = { theme, font, fontSize };
    void saveUserSettings(patch).catch(() => {
      // Best-effort: the change is already applied and in localStorage.
    });
  }, [theme, font, fontSize]);
}
