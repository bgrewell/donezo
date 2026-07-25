/** Registry of built-in themes. A theme is a data-theme attribute value;
 *  its variable overrides live in src/styles/themes.css. */
export const THEMES = [
  { id: "console", label: "GTech Console", hint: "Ink-navy mission control (default)" },
  { id: "slate", label: "Slate", hint: "Neutral charcoal, quieter accents" },
] as const;

export type ThemeId = (typeof THEMES)[number]["id"];

export const DEFAULT_THEME: ThemeId = "console";
export const THEME_STORAGE_KEY = "donezo.theme";
