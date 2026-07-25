/**
 * Small shared bits for the Focus view rows.
 */

/**
 * Hairline row bottom border at ~60% strength. The ae tokens are plain CSS
 * vars, so Tailwind alpha modifiers (border-ae-line/60) generate nothing —
 * mix the token down with color-mix instead.
 */
export const HAIRLINE_ROW =
  "border-b border-[color:color-mix(in_srgb,var(--ae-border)_60%,transparent)]";

/** Compact effort rendering: 2 → "2h", 1.5 → "1.5h", 0.75 → "0.8h". */
export function formatHours(hours: number): string {
  return `${parseFloat(hours.toFixed(1))}h`;
}

/** Approximate total rounded to the nearest half hour: 4.7 → "~4.5h". */
export function formatApproxHours(hours: number): string {
  return `~${parseFloat((Math.round(hours * 2) / 2).toFixed(1))}h`;
}
