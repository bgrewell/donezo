import { cn } from "@grewelltech/console";

import { FONT_SETS, FONT_SIZES, THEMES } from "@/lib/themes";
import { useTheme } from "@/state/ThemeProvider";

/** One row of mutually exclusive choices. */
function ChoiceRow<T extends string>({
  label,
  hint,
  options,
  value,
  onChange,
}: {
  label: string;
  hint?: string;
  options: readonly { readonly id: T; readonly label: string }[];
  value: T;
  onChange: (id: T) => void;
}) {
  return (
    <div>
      <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        {label}
      </div>
      <div role="radiogroup" aria-label={label} className="mt-1.5 flex flex-wrap gap-1.5">
        {options.map((option) => {
          const selected = option.id === value;
          return (
            <button
              key={option.id}
              type="button"
              role="radio"
              aria-checked={selected}
              onClick={() => onChange(option.id)}
              className={cn(
                "rounded-gtc border px-2.5 py-1 font-sans text-[0.8rem] outline-none transition-colors",
                "focus-visible:shadow-gtc-focus",
                selected
                  ? "border-gtc-accent bg-gtc-tint-accent text-gtc-accent"
                  : "border-gtc-line text-gtc-muted hover:border-gtc-accent-dim hover:text-gtc-text"
              )}
            >
              {option.label}
            </button>
          );
        })}
      </div>
      {hint && <p className="m-0 pt-1.5 text-[0.72rem] text-gtc-muted">{hint}</p>}
    </div>
  );
}

/**
 * Appearance: theme, typeface, text size.
 *
 * The same preferences the palette control in the top bar sets — that stays
 * as the one-click path for switching theme mid-thought, since it is the one
 * setting people change while working rather than while configuring. This is
 * where they live alongside everything else, with room to say what they do.
 */
export function AppearanceSection() {
  const { theme, setTheme, font, setFont, fontSize, setFontSize } = useTheme();

  return (
    <section className="space-y-5">
      <ChoiceRow
        label="Theme"
        hint="Themes are token overrides — add more in themes.css."
        options={THEMES}
        value={theme}
        onChange={setTheme}
      />
      <ChoiceRow label="Font" options={FONT_SETS} value={font} onChange={setFont} />
      <ChoiceRow label="Text size" options={FONT_SIZES} value={fontSize} onChange={setFontSize} />
    </section>
  );
}
