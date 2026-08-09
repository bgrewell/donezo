/**
 * Turning captured text into a short form plus an optional long one.
 *
 * Mirrors splitCapture in internal/mcp/handlers.go — the same capture should
 * become the same item whether it is classified in the app or by an agent.
 */

/** A capture split into its short form and the rest.
 *
 *  A capture is very often a first line followed by context, and before tasks
 *  and reminders had anywhere to put context, all of it became the title. The
 *  break has to be a newline the person actually typed: guessing a sentence
 *  boundary inside one long line would be inventing structure they did not
 *  give, so a single line stays whole. */
export function splitCapture(raw: string): { short: string; long: string } {
  const trimmed = raw.trim();
  const br = trimmed.indexOf("\n");
  if (br === -1) return { short: trimmed, long: "" };
  return { short: trimmed.slice(0, br).trim(), long: trimmed.slice(br + 1).trim() };
}
