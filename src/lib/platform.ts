/** True on macOS/iOS — used to render ⌘ vs Ctrl in shortcut hints. */
export const IS_MAC =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/i.test(navigator.platform);

/** Platform-aware chord label: "⌘K" on macOS, "Ctrl+K" elsewhere. Keeps
 *  modifier casing and joiner consistent wherever chords are shown. */
export function modChordLabel(key: string): string {
  return IS_MAC ? `⌘${key}` : `Ctrl+${key}`;
}

/** Shortcut chip label for the quick-capture binding. */
export const CAPTURE_KEY_LABEL = modChordLabel("K");

/** Modifier name used in inline keymap hints. */
export const MOD_LABEL = IS_MAC ? "⌘" : "CTRL";
