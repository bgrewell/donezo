/** True on macOS/iOS — used to render ⌘ vs Ctrl in shortcut hints. */
export const IS_MAC =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/i.test(navigator.platform);

/** Shortcut chip label for the quick-capture binding. */
export const CAPTURE_KEY_LABEL = IS_MAC ? "⌘K" : "Ctrl+K";

/** Modifier name used in inline keymap hints. */
export const MOD_LABEL = IS_MAC ? "⌘" : "CTRL";
