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
 *  reading position for each zoom level. */
export function anchorForToday(zoom: ZoomLevel): string {
  const today = todayISO();
  switch (zoom) {
    case "day":
      // today sits at position 5 of 7 — recent past visible, a little future
      return addDaysISO(today, -4);
    case "week":
      // 5 visible weeks, current week fourth
      return addDaysISO(startOfWeekISO(today), -21);
    case "month":
      // 6 visible months, current month fifth
      return addMonthsISO(startOfMonthISO(today), -4);
    case "quarter":
      // 2 visible quarters, current one second
      return addMonthsISO(startOfQuarterISO(today), -3);
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
