/**
 * Timeline geometry — the single source of truth for the Project Pulse view.
 *
 * Every zoom level renders the same fixed date range so scroll <-> anchor
 * mapping stays stable across zoom changes. Columns are uniform width per
 * zoom; dates inside a column map to a proportional x offset (used for
 * milestone glyphs and the today line).
 */
import { format, getDay, getDaysInMonth, getISOWeek } from "date-fns";

import type { ZoomLevel } from "@/domain/types";
import {
  addDaysISO,
  addMonthsISO,
  diffDays,
  parseDate,
} from "@/lib/time";

/** First rendered day (a Monday). */
export const RANGE_START = "2026-02-23";
/** Last rendered day (inclusive, a Sunday). */
export const RANGE_END = "2026-09-06";

/** First day of the first rendered month column (month/quarter zooms). */
const MONTH_RANGE_START = "2026-02-01";

const DAY_COUNT = diffDays(RANGE_END, RANGE_START) + 1; // 196
const WEEK_COUNT = DAY_COUNT / 7; // 28

export interface ZoomConfig {
  /** Uniform column width, px. */
  colWidth: number;
  /** Project row height, px. */
  rowHeight: number;
  /** Date header height, px. */
  headerHeight: number;
}

export const ZOOM_CONFIG: Record<ZoomLevel, ZoomConfig> = {
  day: { colWidth: 150, rowHeight: 76, headerHeight: 40 },
  week: { colWidth: 232, rowHeight: 60, headerHeight: 40 },
  month: { colWidth: 320, rowHeight: 48, headerHeight: 40 },
  quarter: { colWidth: 156, rowHeight: 48, headerHeight: 52 },
};

/** Day-zoom node capsule metrics (px). */
export const DAY_NODE_HEIGHT = 18;
export const DAY_NODE_GAP = 2;
export const DAY_NODE_INSET = 2;
/** Week-zoom aggregate capsule height (px). */
export const WEEK_CAPSULE_HEIGHT = 30;
/** Month/quarter density bar height (px). */
export const MONTH_BAR_HEIGHT = 10;

export interface TimelineColumn {
  /** First day of the column, ISO yyyy-MM-dd. */
  startISO: string;
  /** Last day of the column (inclusive), ISO yyyy-MM-dd. */
  endISO: string;
  /** Small kicker line (weekday, "WK 30", month name at quarter zoom). */
  label1: string;
  /** Main label ("Jul 25", "Jul 20-26", year at month zoom). */
  label2: string;
  /** Day zoom only: Saturday/Sunday column. */
  isWeekend: boolean;
}

const columnCache = new Map<ZoomLevel, TimelineColumn[]>();

/** Column descriptors for a zoom level (cached — the range is fixed). */
export function columns(zoom: ZoomLevel): TimelineColumn[] {
  const cached = columnCache.get(zoom);
  if (cached) return cached;
  const cols: TimelineColumn[] = [];
  if (zoom === "day") {
    for (let i = 0; i < DAY_COUNT; i++) {
      const iso = addDaysISO(RANGE_START, i);
      const d = parseDate(iso);
      const dow = getDay(d);
      cols.push({
        startISO: iso,
        endISO: iso,
        label1: format(d, "EEE"),
        label2: format(d, "MMM d"),
        isWeekend: dow === 0 || dow === 6,
      });
    }
  } else if (zoom === "week") {
    for (let i = 0; i < WEEK_COUNT; i++) {
      const startISO = addDaysISO(RANGE_START, i * 7);
      const endISO = addDaysISO(startISO, 6);
      const s = parseDate(startISO);
      const e = parseDate(endISO);
      const sameMonth = format(s, "MMM") === format(e, "MMM");
      cols.push({
        startISO,
        endISO,
        label1: `WK ${getISOWeek(s)}`,
        label2: `${format(s, "MMM d")}-${format(e, sameMonth ? "d" : "MMM d")}`,
        isWeekend: false,
      });
    }
  } else {
    let startISO = MONTH_RANGE_START;
    while (startISO <= RANGE_END) {
      const nextISO = addMonthsISO(startISO, 1);
      const d = parseDate(startISO);
      cols.push({
        startISO,
        endISO: addDaysISO(nextISO, -1),
        label1: zoom === "month" ? format(d, "MMMM") : format(d, "MMM"),
        label2: zoom === "month" ? format(d, "yyyy") : "",
        isWeekend: false,
      });
      startISO = nextISO;
    }
  }
  columnCache.set(zoom, cols);
  return cols;
}

export interface QuarterBand {
  label: string;
  monthSpan: number;
}

/** Quarter header band segments ("Q1 2026" spanning its month columns). */
export function quarterBands(): QuarterBand[] {
  const bands: QuarterBand[] = [];
  for (const col of columns("quarter")) {
    const d = parseDate(col.startISO);
    const label = `Q${Math.floor(d.getMonth() / 3) + 1} ${d.getFullYear()}`;
    const last = bands[bands.length - 1];
    if (last && last.label === label) last.monthSpan += 1;
    else bands.push({ label, monthSpan: 1 });
  }
  return bands;
}

/** Full scrollable track width for a zoom level, px. */
export function totalWidth(zoom: ZoomLevel): number {
  return columns(zoom).length * ZOOM_CONFIG[zoom].colWidth;
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(Math.max(v, lo), hi);
}

/** Month-column index for a date (month/quarter zooms); may be out of range. */
export function monthColIndex(iso: string): number {
  const d = parseDate(iso);
  const s = parseDate(MONTH_RANGE_START);
  return (d.getFullYear() - s.getFullYear()) * 12 + (d.getMonth() - s.getMonth());
}

/** Pixel offset of a date. Day zoom is day-accurate; other zooms place the
 *  date proportionally within its column (for milestones / today line). */
export function xForDate(iso: string, zoom: ZoomLevel): number {
  const w = ZOOM_CONFIG[zoom].colWidth;
  if (zoom === "day" || zoom === "week") {
    const days = diffDays(iso, RANGE_START);
    const x = zoom === "day" ? days * w : (days / 7) * w;
    return clamp(x, 0, totalWidth(zoom));
  }
  const d = parseDate(iso);
  const frac = (d.getDate() - 1) / getDaysInMonth(d);
  return clamp((monthColIndex(iso) + frac) * w, 0, totalWidth(zoom));
}

/** Date at a pixel offset. Day zoom: that day; other zooms: the start date
 *  of the column under x (used for click-to-create and anchor tracking). */
export function dateAtX(x: number, zoom: ZoomLevel): string {
  const w = ZOOM_CONFIG[zoom].colWidth;
  const idx = clamp(Math.floor(x / w), 0, columns(zoom).length - 1);
  if (zoom === "day") return addDaysISO(RANGE_START, idx);
  if (zoom === "week") return addDaysISO(RANGE_START, idx * 7);
  return addMonthsISO(MONTH_RANGE_START, idx);
}

/** Controls-bar label for the window starting at the anchor date. */
export function visibleRangeLabel(anchor: string, zoom: ZoomLevel): string {
  if (zoom === "day" || zoom === "week") {
    const end = addDaysISO(anchor, zoom === "day" ? 6 : 34);
    const s = parseDate(anchor);
    const e = parseDate(end);
    if (format(s, "yyyy") !== format(e, "yyyy")) {
      return `${format(s, "MMM d, yyyy")} - ${format(e, "MMM d, yyyy")}`;
    }
    return `${format(s, "MMM d")} - ${format(e, "MMM d, yyyy")}`;
  }
  const end = addMonthsISO(anchor, 5);
  const s = parseDate(anchor);
  const e = parseDate(end);
  if (format(s, "yyyy") !== format(e, "yyyy")) {
    return `${format(s, "MMM yyyy")} - ${format(e, "MMM yyyy")}`;
  }
  return `${format(s, "MMM")} - ${format(e, "MMM yyyy")}`;
}

/** Rail cell width CSS expression for the current collapse state. */
export function railWidth(collapsed: boolean): string {
  return collapsed ? "44px" : "var(--dz-rail-w)";
}

/** Column grid hairline color (kept subtle — ~half-strength border token). */
export const GRID_LINE = "color-mix(in srgb, var(--ae-border) 55%, transparent)";

/** Row divider color (border token at reduced opacity). */
export const ROW_BORDER = "color-mix(in srgb, var(--ae-border) 45%, transparent)";

/** Weekend wash for day-zoom header cells and body columns. Neutral darkening
 *  (bg-deep alone is too close to the page bg to read as a wash). */
export const WEEKEND_WASH =
  "color-mix(in srgb, color-mix(in srgb, var(--ae-bg-deep) 40%, black) 45%, transparent)";

/** Project-color tint, e.g. mixProject("var(--dz-pj-blue)", 16). */
export function mixProject(colorVar: string, pct: number): string {
  return `color-mix(in srgb, ${colorVar} ${pct}%, transparent)`;
}

/** Background image stack for a row's timeline surface: column grid lines,
 *  plus the weekend wash at day zoom (crisper than per-column divs). */
export function rowBackgroundImage(zoom: ZoomLevel): string {
  const w = ZOOM_CONFIG[zoom].colWidth;
  const grid = `repeating-linear-gradient(to right, ${GRID_LINE} 0px, ${GRID_LINE} 1px, transparent 1px, transparent ${w}px)`;
  if (zoom !== "day") return grid;
  const weekend = `repeating-linear-gradient(to right, transparent 0px, transparent ${w * 5}px, ${WEEKEND_WASH} ${w * 5}px, ${WEEKEND_WASH} ${w * 7}px)`;
  return `${grid}, ${weekend}`;
}
