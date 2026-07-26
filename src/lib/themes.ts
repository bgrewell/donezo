/** Registry of built-in themes. A theme is a data-theme attribute value;
 *  its variable overrides live in src/styles/themes.css. */
export const THEMES = [
  { id: "console", label: "GTech Console", hint: "Ink-navy mission control (default)" },
  { id: "slate", label: "Slate", hint: "Neutral charcoal, quieter accents" },
  { id: "paper", label: "Paper", hint: "Warm light, ink on paper" },
  { id: "blossom", label: "Blossom", hint: "Soft blush, raspberry accent" },
] as const;

export type ThemeId = (typeof THEMES)[number]["id"];

export const DEFAULT_THEME: ThemeId = "console";
export const THEME_STORAGE_KEY = "donezo.theme";

/** Font sets — a data-font attribute value; the variable overrides live in
 *  src/styles/app.css (plex is the token default, so it has no block). */
export const FONT_SETS = [
  { id: "plex", label: "IBM Plex", hint: "The house voice (default)" },
  { id: "inter", label: "Inter + JetBrains", hint: "Rounder sans, relaxed mono" },
  { id: "system", label: "System", hint: "Native fonts, zero download" },
] as const;

export type FontSetId = (typeof FONT_SETS)[number]["id"];

export const DEFAULT_FONT: FontSetId = "plex";
export const FONT_STORAGE_KEY = "donezo.font";

/** Text sizes — a data-fontsize attribute value scaling the rem root;
 *  medium is the browser default (16px), so it has no CSS block. */
export const FONT_SIZES = [
  { id: "small", label: "Small" },
  { id: "medium", label: "Medium" },
  { id: "large", label: "Large" },
] as const;

export type FontSizeId = (typeof FONT_SIZES)[number]["id"];

export const DEFAULT_FONT_SIZE: FontSizeId = "medium";
export const FONT_SIZE_STORAGE_KEY = "donezo.fontsize";
