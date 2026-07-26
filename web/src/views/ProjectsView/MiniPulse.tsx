import * as React from "react";

import type { ActivityEntry, Project } from "@/domain/types";
import { projectColorVar } from "@/lib/projectColors";
import { addDaysISO, formatDay, startOfWeekISO, todayISO } from "@/lib/time";
import { Tip } from "@/components/ui/Tooltip";

/** Days shown in the strip, ending today. */
const DAY_COUNT = 56;
/** Square node edge / bar width, px. */
const NODE = 6;
/** Max bar height for busy days, px. */
const MAX_BAR = 24;

interface DayBucket {
  iso: string;
  /** Horizontal center, percent of strip width. */
  pos: number;
  actual: ActivityEntry[];
  planned: ActivityEntry[];
  isMonday: boolean;
}

/**
 * Self-contained activity pulse for one project: the last 56 days as a
 * fixed strip. Days with entries render a square node (a taller bar when
 * several entries landed on one day); planned entries show as dashed
 * outlines when the planned layer is on. Clicking a node inspects the
 * first entry of that day.
 */
export function MiniPulse({
  project,
  entries,
  showPlanned,
  onSelectEntry,
}: {
  project: Project;
  entries: ActivityEntry[];
  showPlanned: boolean;
  onSelectEntry: (id: string) => void;
}) {
  const today = todayISO();

  const days = React.useMemo(() => {
    const start = addDaysISO(today, -(DAY_COUNT - 1));
    const byDate = new Map<string, { actual: ActivityEntry[]; planned: ActivityEntry[] }>();
    for (const e of entries) {
      if (e.date < start || e.date > today) continue;
      let bucket = byDate.get(e.date);
      if (!bucket) {
        bucket = { actual: [], planned: [] };
        byDate.set(e.date, bucket);
      }
      (e.planned ? bucket.planned : bucket.actual).push(e);
    }
    const out: DayBucket[] = [];
    for (let i = 0; i < DAY_COUNT; i++) {
      const iso = addDaysISO(start, i);
      const bucket = byDate.get(iso);
      out.push({
        iso,
        pos: ((i + 0.5) / DAY_COUNT) * 100,
        actual: bucket?.actual ?? [],
        planned: bucket?.planned ?? [],
        isMonday: startOfWeekISO(iso) === iso,
      });
    }
    return out;
  }, [entries, today]);

  const color = projectColorVar(project.color);
  const mondays = days.filter((d) => d.isMonday);
  const todayPos = days[DAY_COUNT - 1].pos;
  const hasNodes = days.some(
    (d) => d.actual.length > 0 || (showPlanned && d.planned.length > 0)
  );

  return (
    <div>
      <div className="relative h-14 overflow-hidden rounded-gtc border border-gtc-line bg-gtc-inset">
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
            No activity in the last {DAY_COUNT} days
          </span>
        )}
        {days.map((d) => {
          const hasActual = d.actual.length > 0;
          const plannedOnly = !hasActual && showPlanned && d.planned.length > 0;
          if (!hasActual && !plannedOnly) return null;

          const list = hasActual ? d.actual : d.planned;
          const count = list.length;
          const first = list[0];
          const height = count > 1 ? Math.min(NODE * count, MAX_BAR) : NODE;
          const noun = count === 1 ? "entry" : "entries";

          return (
            <Tip
              key={d.iso}
              content={
                <div className="max-w-[240px]">
                  <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                    {formatDay(d.iso)} · {count} {noun}
                    {plannedOnly && " · planned"}
                  </div>
                  <div className="truncate text-[0.78rem]">{first.title}</div>
                </div>
              }
            >
              <button
                type="button"
                onClick={() => onSelectEntry(first.id)}
                aria-label={`${formatDay(d.iso)}: ${count} ${noun}${plannedOnly ? " planned" : ""}`}
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
          .filter((_, i) => i % 2 === 0)
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
