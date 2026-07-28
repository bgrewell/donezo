import * as React from "react";
import {
  DEFAULT_FONT,
  DEFAULT_FONT_SIZE,
  DEFAULT_THEME,
  FONT_SETS,
  FONT_SIZE_STORAGE_KEY,
  FONT_SIZES,
  FONT_STORAGE_KEY,
  THEME_STORAGE_KEY,
  THEMES,
  type FontSetId,
  type FontSizeId,
  type ThemeId,
} from "@/lib/themes";

interface ThemeContextValue {
  theme: ThemeId;
  setTheme: (id: ThemeId) => void;
  font: FontSetId;
  setFont: (id: FontSetId) => void;
  fontSize: FontSizeId;
  setFontSize: (id: FontSizeId) => void;
}

const ThemeContext = React.createContext<ThemeContextValue | null>(null);

function loadStoredPref<T extends string>(
  key: string,
  options: readonly { id: T }[],
  fallback: T
): T {
  try {
    const stored = window.localStorage.getItem(key);
    if (stored && options.some((o) => o.id === stored)) return stored as T;
  } catch {
    // localStorage unavailable (private mode etc.) — fall through to default
  }
  return fallback;
}

/** Applies one appearance pref as a data-* attribute on <html> and persists it. */
function applyPref(datasetKey: string, storageKey: string, value: string) {
  document.documentElement.dataset[datasetKey] = value;
  try {
    window.localStorage.setItem(storageKey, value);
  } catch {
    // persistence is best-effort
  }
}

/** Applies the active theme, font set, and text size as data-theme /
 *  data-font / data-fontsize on <html> and persists each preference. */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setTheme] = React.useState<ThemeId>(() =>
    loadStoredPref(THEME_STORAGE_KEY, THEMES, DEFAULT_THEME)
  );
  const [font, setFont] = React.useState<FontSetId>(() =>
    loadStoredPref(FONT_STORAGE_KEY, FONT_SETS, DEFAULT_FONT)
  );
  const [fontSize, setFontSize] = React.useState<FontSizeId>(() =>
    loadStoredPref(FONT_SIZE_STORAGE_KEY, FONT_SIZES, DEFAULT_FONT_SIZE)
  );

  React.useEffect(() => {
    applyPref("theme", THEME_STORAGE_KEY, theme);
  }, [theme]);

  React.useEffect(() => {
    applyPref("font", FONT_STORAGE_KEY, font);
  }, [font]);

  React.useEffect(() => {
    applyPref("fontsize", FONT_SIZE_STORAGE_KEY, fontSize);
  }, [fontSize]);

  const value = React.useMemo(
    () => ({ theme, setTheme, font, setFont, fontSize, setFontSize }),
    [theme, font, fontSize]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = React.useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
