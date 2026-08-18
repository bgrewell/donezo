import * as React from "react";
import { Bell } from "lucide-react";
import { cn } from "@grewelltech/console";

import { addDaysISO, formatDay, relativeFromToday, todayISO } from "@/lib/time";
import { repeatPhrase } from "@/lib/repeat";
import { projectColorVar } from "@/lib/projectColors";
import type { HorizonMarker } from "./useFocusData";
import { HORIZON_DAYS } from "./useFocusData";

/** Where "now" sits on the strip (percent from the left). Everything to its
 *  left is the overdue gutter; the plotted future runs from here to the right
 *  edge. The now line is the tense divider — the same rule the Timeline uses. */
const NOW_X = 11;
/** Small gap so a "today" bell (offset 0) sits just clear of the now line
 *  instead of colliding with its bead. */
const NOW_GAP = 3;
/** Right edge the +HORIZON_DAYS mark lands on, leaving room for the bell. */
const RIGHT_X = 96;
/** At most this many overdue bells before collapsing the rest into "+N". */
const OVERDUE_SHOWN = 3;

const HAIRLINE = "color-mix(in srgb, var(--gtc-border) 60%, transparent)";

/** Percent-x for a day offset from today (0 = just right of now, HORIZON_DAYS =
 *  right edge). Upcoming markers start a hair past the now line so "today" is
 *  legible against it. */
function xForOffset(offset: number): number {
  const clamped = Math.min(Math.max(offset, 0), HORIZON_DAYS);
  return NOW_X + NOW_GAP + (clamped / HORIZON_DAYS) * (RIGHT_X - NOW_X - NOW_GAP);
}

/** One bell: quiet in the project's colour when upcoming, urgent (danger, a
 *  slow pulse) when overdue. A recurring reminder wears a small repeat dot. */
function HorizonBell({ marker }: { marker: HorizonMarker }) {
  const { overdue, project, repeat } = marker;
  const repeats = repeat ? ` · repeats ${repeatPhrase(repeat)}` : "";
  const when = overdue ? "overdue" : relativeFromToday(marker.due);
  return (
    <span
      className={cn(
        "relative flex items-center justify-center",
        overdue && "text-gtc-danger"
      )}
      style={
        overdue
          ? undefined
          : { color: project ? projectColorVar(project.color) : "var(--gtc-muted)" }
      }
      title={`${marker.title} — ${when}${repeats}`}
    >
      <Bell className={cn("h-3.5 w-3.5", overdue && "animate-pulse")} aria-hidden />
      {repeat && (
        <span
          className="absolute -right-0.5 -top-0.5 h-[5px] w-[5px] rounded-full ring-1 ring-[color:var(--gtc-bg)]"
          style={{ background: "currentColor" }}
        />
      )}
    </span>
  );
}

/** The Focus "now" strip: a slim, glanceable axis with **now** anchored, the
 *  near-future plotted as bells to its right and anything overdue clustered in
 *  the gutter to its left. It is a *picture of when*, not a second action list.
 *  The visual axis is decorative (aria-hidden); a visually-hidden list carries
 *  the same reminders for assistive tech, because the strip reaches further out
 *  (HORIZON_DAYS) than the Time-sensitive section does. Renders nothing when
 *  the horizon is clear. */
export function HorizonStrip({ horizon }: { horizon: HorizonMarker[] }) {
  const overdue = horizon.filter((m) => m.overdue);
  const upcoming = horizon.filter((m) => !m.overdue);

  // Group upcoming by day so several bells on one date fan out side by side
  // instead of stacking into a single blob.
  const byDay = React.useMemo(() => {
    const map = new Map<string, HorizonMarker[]>();
    for (const m of upcoming) {
      const list = map.get(m.due);
      if (list) list.push(m);
      else map.set(m.due, [m]);
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [upcoming]);

  if (horizon.length === 0) return null;

  const shownOverdue = overdue.slice(0, OVERDUE_SHOWN);
  const extraOverdue = overdue.length - shownOverdue.length;
  const ticks = [7, HORIZON_DAYS].map((off) => ({
    off,
    iso: addDaysISO(todayISO(), off),
  }));

  return (
    <section aria-label="On the horizon" className="select-none">
      {/* Accessible copy of what the axis plots — the visual strip below is
          aria-hidden, and it reaches further out than the Time-sensitive list,
          so this list is the only accessible home for the 8–14 day band. */}
      <ul className="sr-only">
        {horizon.map((m) => (
          <li key={m.id}>
            {m.title} — {m.overdue ? "overdue" : relativeFromToday(m.due)}
            {m.project ? `, ${m.project.name}` : ""}
            {m.repeat ? `, repeats ${repeatPhrase(m.repeat)}` : ""}
          </li>
        ))}
      </ul>

      <div aria-hidden>
      <div className="mb-2 flex items-baseline justify-between font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        <span>On the horizon</span>
        <span className={cn(overdue.length > 0 && "text-gtc-danger")}>
          {overdue.length > 0 ? `${overdue.length} overdue` : `next ${HORIZON_DAYS} days`}
        </span>
      </div>

      <div className="relative h-11 overflow-hidden">
        {/* the axis */}
        <div
          className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2"
          style={{ background: HAIRLINE }}
        />
        {/* now line + bead */}
        <div
          className="absolute bottom-0 top-0 w-px"
          style={{ left: `${NOW_X}%`, background: "var(--gtc-accent)", opacity: 0.5 }}
        />
        <div
          className="absolute top-1/2 h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full"
          style={{ left: `${NOW_X}%`, background: "var(--gtc-accent)" }}
        />

        {/* overdue gutter — pinned just left of the now line, growing leftward */}
        {overdue.length > 0 && (
          <div
            className="absolute top-1/2 flex -translate-y-1/2 items-center gap-1"
            style={{ right: `calc(${100 - NOW_X}% + 6px)` }}
          >
            {shownOverdue.map((m) => (
              <HorizonBell key={m.id} marker={m} />
            ))}
            {extraOverdue > 0 && (
              <span className="font-mono text-[0.6rem] font-medium text-gtc-danger">
                +{extraOverdue}
              </span>
            )}
          </div>
        )}

        {/* upcoming — one centred cluster per day */}
        {byDay.map(([due, items]) => (
          <div
            key={due}
            className="absolute top-1/2 flex -translate-x-1/2 -translate-y-1/2 items-center gap-0.5"
            style={{ left: `${xForOffset(items[0].offset)}%` }}
          >
            {items.map((m) => (
              <HorizonBell key={m.id} marker={m} />
            ))}
          </div>
        ))}
      </div>

      {/* tick labels */}
      <div className="relative mt-1 h-3 overflow-hidden font-mono text-[0.58rem] uppercase tracking-label text-gtc-muted">
        <span className="absolute -translate-x-1/2" style={{ left: `${NOW_X}%` }}>
          now
        </span>
        {ticks.map((t) => (
          <span
            key={t.off}
            className="absolute -translate-x-1/2 whitespace-nowrap"
            style={{ left: `${xForOffset(t.off)}%` }}
          >
            {formatDay(t.iso)}
          </span>
        ))}
      </div>
      </div>
    </section>
  );
}
