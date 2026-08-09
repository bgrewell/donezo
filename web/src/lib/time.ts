import {
  addDays,
  addMonths,
  differenceInCalendarDays,
  format,
  parseISO,
  startOfMonth,
  startOfQuarter,
  startOfWeek,
} from "date-fns";

import type { ZoomLevel } from "@/domain/types";

/** First rendered timeline day (a Monday). */
export const RANGE_START = "2026-02-23";
/** Last rendered timeline day (inclusive, a Sunday). */
export const RANGE_END = "2026-09-06";

/** Parse an ISO yyyy-MM-dd string as a local date. */
export function parseDate(iso: string): Date {
  return parseISO(iso);
}

export function toISODate(d: Date): string {
  return format(d, "yyyy-MM-dd");
}

export function todayISO(): string {
  return toISODate(new Date());
}

/** Local wall-clock timestamp without zone info: "2026-07-25T18:30:05".
 *  All naive datetimes in the app (capturedAt, remindAt) are local time —
 *  never use Date.toISOString(), which silently shifts to UTC. */
export function nowLocalISO(): string {
  return format(new Date(), "yyyy-MM-dd'T'HH:mm:ss");
}

/** Normalize a datetime-local input value to the seconds-bearing local ISO
 *  the API expects (2026-07-26T09:00 → 2026-07-26T09:00:00). */
export function withSeconds(dt: string): string {
  return dt.length === 16 ? `${dt}:00` : dt;
}

/** The local calendar day an *instant* falls on.
 *
 *  donezo stores two kinds of time and they are not interchangeable. Naive
 *  local datetimes — capturedAt, remindAt — are already local, so slicing
 *  their first ten characters gives the right day. True instants — the
 *  RFC 3339 UTC timestamps on API tokens and invites — are not: slicing one
 *  yields the day it was in *UTC*, which for anyone west of Greenwich is
 *  tomorrow for the last hours of every day. Convert, don't slice. */
export function localDayOfInstant(instant: string): string {
  return toISODate(new Date(instant));
}

/** relativeFromToday for a true instant rather than a local day.
 *  See localDayOfInstant for why the two cannot share a code path. */
export function relativeFromInstant(instant: string): string {
  return relativeFromToday(localDayOfInstant(instant));
}

export function addDaysISO(iso: string, n: number): string {
  return toISODate(addDays(parseDate(iso), n));
}

export function addMonthsISO(iso: string, n: number): string {
  return toISODate(addMonths(parseDate(iso), n));
}

/** Monday-start week. */
export function startOfWeekISO(iso: string): string {
  return toISODate(startOfWeek(parseDate(iso), { weekStartsOn: 1 }));
}

export function startOfMonthISO(iso: string): string {
  return toISODate(startOfMonth(parseDate(iso)));
}

export function startOfQuarterISO(iso: string): string {
  return toISODate(startOfQuarter(parseDate(iso)));
}

/** Calendar-day difference a − b. */
export function diffDays(a: string, b: string): number {
  return differenceInCalendarDays(parseDate(a), parseDate(b));
}

export function isToday(iso: string): boolean {
  return iso === todayISO();
}

/** "Jul 25" */
export function formatDay(iso: string): string {
  return format(parseDate(iso), "MMM d");
}

/** "Sat" */
export function formatWeekday(iso: string): string {
  return format(parseDate(iso), "EEE");
}

/** "July 2026" */
export function formatMonth(iso: string): string {
  return format(parseDate(iso), "MMMM yyyy");
}

/** "Saturday, July 25, 2026" */
export function formatFull(iso: string): string {
  return format(parseDate(iso), "EEEE, MMMM d, yyyy");
}

/** Compact relative phrasing for rails and lists: "today", "yesterday",
 *  "4d ago", "3w ago", "2mo ago", "tomorrow", "in 5d", … */
export function relativeFromToday(iso: string): string {
  const d = diffDays(todayISO(), iso);
  if (d === 0) return "today";
  if (d > 0) {
    if (d === 1) return "yesterday";
    if (d < 7) return `${d}d ago`;
    if (d < 30) return `${Math.round(d / 7)}w ago`;
    if (d < 365) return `${Math.round(d / 30)}mo ago`;
    return `${Math.round(d / 365)}y ago`;
  }
  const f = -d;
  if (f === 1) return "tomorrow";
  if (f < 7) return `in ${f}d`;
  if (f < 30) return `in ${Math.round(f / 7)}w`;
  return `in ${Math.round(f / 30)}mo`;
}

/** Anchor (left edge of the visible window) that puts today in a natural
 *  reading position for each zoom level.
 *
 *  `visibleColumns` is how many whole columns the timeline lane can show
 *  (measured, or estimated at boot). The designed offsets assume a desktop
 *  lane; on narrower lanes the past-context offset shrinks so today's own
 *  column always stays on screen. Omitting it keeps the designed positions,
 *  and lanes wide enough for the designed window are unaffected either way. */
export function anchorForToday(zoom: ZoomLevel, visibleColumns?: number): string {
  const today = todayISO();
  // Columns of past context before today's column: the designed amount,
  // capped at visibleColumns - 1 so today itself is never pushed off.
  const past = (designed: number) =>
    Math.min(designed, Math.max(0, (visibleColumns ?? Number.POSITIVE_INFINITY) - 1));
  switch (zoom) {
    case "day":
      // today sits at position 5 of 7 — recent past visible, a little future
      return addDaysISO(today, -past(4));
    case "week":
      // 5 visible weeks, current week fourth
      return addDaysISO(startOfWeekISO(today), -7 * past(3));
    case "month":
      // ~5 visible months, current month fourth
      return addMonthsISO(startOfMonthISO(today), -past(3));
    case "quarter":
      // 2 visible quarters, current one second. Below 6 month columns the
      // quarter-start offset can strand today off screen (today may sit up
      // to 2 months into its quarter), so anchor to today's month instead.
      if (visibleColumns === undefined || visibleColumns >= 6) {
        return addMonthsISO(startOfQuarterISO(today), -3);
      }
      return addMonthsISO(startOfMonthISO(today), -past(2));
  }
}

/** How far previous/next page for each zoom level, in days or months. */
export function shiftAnchor(anchor: string, zoom: ZoomLevel, dir: 1 | -1): string {
  switch (zoom) {
    case "day":
      return addDaysISO(anchor, dir * 7);
    case "week":
      return addDaysISO(anchor, dir * 28);
    case "month":
      return addMonthsISO(anchor, dir * 3);
    case "quarter":
      return addMonthsISO(anchor, dir * 3);
  }
}

/** Conservative visible-window span per zoom, in days. Sized to a typical
 *  desktop viewport so anchors stop where the window would leave the range. */
const VISIBLE_WINDOW_DAYS: Record<ZoomLevel, number> = {
  day: 7,
  week: 35,
  month: 150,
  quarter: 240,
};

/** Days spanned by one column at each zoom (month/quarter columns are
 *  calendar months; 31 is the safe upper bound so a measured window never
 *  overshoots the range). Converts a measured column count into a window. */
const DAYS_PER_COLUMN: Record<ZoomLevel, number> = {
  day: 1,
  week: 7,
  month: 31,
  quarter: 31,
};

/** Clamp a date into the rendered range [RANGE_START, RANGE_END]. */
export function clampToRange(iso: string): string {
  if (iso < RANGE_START) return RANGE_START;
  if (iso > RANGE_END) return RANGE_END;
  return iso;
}

/** Clamp an anchor so its visible window stays inside the rendered range:
 *  [RANGE_START, latest anchor whose window still ends by RANGE_END].
 *  `visibleColumns` (when measured) sizes the window from the real lane —
 *  the desktop-tuned defaults over-clamp on phones, where a much smaller
 *  window fits and later anchors are therefore still fully in range. */
export function clampAnchor(
  iso: string,
  zoom: ZoomLevel,
  visibleColumns?: number
): string {
  const windowDays =
    visibleColumns !== undefined
      ? visibleColumns * DAYS_PER_COLUMN[zoom]
      : VISIBLE_WINDOW_DAYS[zoom];
  let max = addDaysISO(RANGE_END, 1 - windowDays);
  if (zoom === "month" || zoom === "quarter") {
    // Month/quarter anchors sit on month starts; round the ceiling up so the
    // last reachable window still includes the final columns.
    const monthStart = startOfMonthISO(max);
    if (monthStart !== max) max = addMonthsISO(monthStart, 1);
  }
  if (max < RANGE_START) max = RANGE_START;
  if (iso > max) return max;
  if (iso < RANGE_START) return RANGE_START;
  return iso;
}
