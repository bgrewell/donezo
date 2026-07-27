import * as React from "react";

import type { ActivityEntry, Project } from "@/domain/types";
import { projectColorVar } from "@/lib/projectColors";
import { addDaysISO, diffDays, formatDay, startOfWeekISO, todayISO } from "@/lib/time";
import { Tip } from "@/components/ui/Tooltip";

/** Default days shown in the strip, ending today (8 weeks). */
export const DEFAULT_DAY_COUNT = 56;
/** Square node edge / bar width, px. */
const NODE = 6;
/** Max bar height for busy days, px. */
const MAX_BAR = 24;
/** Below this per-day width the strip aggregates to per-week bars. */
const MIN_DAY_PX = 3;

interface Bucket {
  /** Bucket key date: the day itself, or the Monday of the week. */
  iso: string;
  /** Horizontal center, percent of strip width. */
  pos: number;
  actual: ActivityEntry[];
  planned: ActivityEntry[];
  isMonday: boolean;
}

/**
 * Self-contained activity pulse for one project: the last `dayCount` days
 * as a fixed strip. Days with entries render a square node (a taller bar
 * when several entries landed on one day); planned entries show as dashed
 * outlines when the planned layer is on. Clicking a node inspects the
 * first entry of that bucket. Wide windows (26W / ALL) aggregate to
 * per-week bars once the per-day slot would fall under 3px.
 */
export function MiniPulse({
  project,
  entries,
  showPlanned,
  onSelectEntry,
  dayCount = DEFAULT_DAY_COUNT,
}: {
  project: Project;
  entries: ActivityEntry[];
  showPlanned: boolean;
  onSelectEntry: (id: string) => void;
  /** Days in the window, ending today. */
  dayCount?: number;
}) {
  const today = todayISO();
  const start = addDaysISO(today, -(dayCount - 1));

  // Measured strip width decides day vs week density. Starts null (first
  // paint uses daily) and settles on the first observe tick.
  const stripRef = React.useRef<HTMLDivElement>(null);
  const [stripWidth, setStripWidth] = React.useState<number | null>(null);
  React.useEffect(() => {
    const el = stripRef.current;
    if (!el) return;
    const update = () => setStripWidth(el.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const weekly = stripWidth !== null && stripWidth / dayCount < MIN_DAY_PX;

  const posForIndex = (i: number) => ((i + 0.5) / dayCount) * 100;
  /** Day slot of an in-window ISO date (0 = window start). */
  const indexOf = (iso: string) =>
    Math.max(0, Math.min(dayCount - 1, diffDays(iso, start)));

  const buckets = React.useMemo(() => {
    const byKey = new Map<string, { actual: ActivityEntry[]; planned: ActivityEntry[] }>();
    for (const e of entries) {
      if (e.date < start || e.date > today) continue;
      const key = weekly ? startOfWeekISO(e.date) : e.date;
      let bucket = byKey.get(key);
      if (!bucket) {
        bucket = { actual: [], planned: [] };
        byKey.set(key, bucket);
      }
      (e.planned ? bucket.planned : bucket.actual).push(e);
    }
    const out: Bucket[] = [];
    if (weekly) {
      for (
        let week = startOfWeekISO(start);
        week <= today;
        week = addDaysISO(week, 7)
      ) {
        const first = week < start ? start : week;
        const last = addDaysISO(week, 6) > today ? today : addDaysISO(week, 6);
        const bucket = byKey.get(week);
        out.push({
          iso: week,
          pos: posForIndex((indexOf(first) + indexOf(last)) / 2),
          actual: bucket?.actual ?? [],
          planned: bucket?.planned ?? [],
          isMonday: true,
        });
      }
    } else {
      for (let i = 0; i < dayCount; i++) {
        const iso = addDaysISO(start, i);
        const bucket = byKey.get(iso);
        out.push({
          iso,
          pos: posForIndex(i),
          actual: bucket?.actual ?? [],
          planned: bucket?.planned ?? [],
          isMonday: startOfWeekISO(iso) === iso,
        });
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entries, today, start, dayCount, weekly]);

  const color = projectColorVar(project.color);

  // Monday gridlines/labels span the window at both densities.
  const mondays = React.useMemo(() => {
    const out: { iso: string; pos: number }[] = [];
    let week = startOfWeekISO(start);
    if (week < start) week = addDaysISO(week, 7); // first Monday inside the window
    for (; week <= today; week = addDaysISO(week, 7)) {
      out.push({ iso: week, pos: posForIndex(indexOf(week)) });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [start, today, dayCount]);
  /** Label every nth Monday so ~7 labels fit at any window size. */
  const labelStride = Math.max(1, Math.ceil(mondays.length / 7));

  const todayPos = posForIndex(dayCount - 1);
  const hasNodes = buckets.some(
    (d) => d.actual.length > 0 || (showPlanned && d.planned.length > 0)
  );

  return (
    <div>
      <div
        ref={stripRef}
        className="relative h-14 overflow-hidden rounded-gtc border border-gtc-line bg-gtc-inset"
      >
        {mondays.map((d) => (
          <span
            key={d.iso}
            aria-hidden
            className="absolute inset-y-0 w-px bg-gtc-line opacity-60"
            style={{ left: `${d.pos}%` }}
          />
        ))}
        <span
          aria-hidden
          className="absolute inset-y-0 w-px bg-gtc-accent"
          style={{ left: `${todayPos}%` }}
        />
        {!hasNodes && (
          <span className="absolute inset-0 flex items-center justify-center font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            No activity in the last {dayCount} days
          </span>
        )}
        {buckets.map((d) => {
          const hasActual = d.actual.length > 0;
          const plannedOnly = !hasActual && showPlanned && d.planned.length > 0;
          if (!hasActual && !plannedOnly) return null;

          const list = hasActual ? d.actual : d.planned;
          const count = list.length;
          const first = list[0];
          const height = count > 1 ? Math.min(NODE * count, MAX_BAR) : NODE;
          const noun = count === 1 ? "entry" : "entries";
          const when = weekly ? `Week of ${formatDay(d.iso)}` : formatDay(d.iso);

          return (
            <Tip
              key={d.iso}
              content={
                <div className="max-w-[240px]">
                  <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                    {when} · {count} {noun}
                    {plannedOnly && " · planned"}
                  </div>
                  <div className="truncate text-[0.78rem]">{first.title}</div>
                </div>
              }
            >
              <button
                type="button"
                onClick={() => onSelectEntry(first.id)}
                aria-label={`${when}: ${count} ${noun}${plannedOnly ? " planned" : ""}`}
                className="absolute top-1/2 flex h-8 w-3.5 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-gtc outline-none focus-visible:shadow-gtc-focus"
                style={{ left: `${d.pos}%` }}
              >
                <span
                  aria-hidden
                  className="block"
                  style={
                    plannedOnly
                      ? { width: NODE, height: NODE, border: `1px dashed ${color}` }
                      : { width: NODE, height, background: color }
                  }
                />
              </button>
            </Tip>
          );
        })}
      </div>

      <div className="relative mt-1 h-4" aria-hidden>
        {mondays
          .filter((_, i) => i % labelStride === 0)
          .map((d) => (
            <span
              key={d.iso}
              className="absolute -translate-x-1/2 whitespace-nowrap font-mono text-[0.6rem] uppercase tracking-label text-gtc-muted"
              style={{ left: `${d.pos}%` }}
            >
              {formatDay(d.iso)}
            </span>
          ))}
      </div>
    </div>
  );
}
