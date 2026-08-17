import { cn } from "@grewelltech/console";

import type { Reminder, ZoomLevel } from "@/domain/types";
import { todayISO } from "@/lib/time";
import { repeatPhrase } from "@/lib/repeat";
import { REMINDER_BAND, ZOOM_CONFIG, rowBackgroundImage, xForDate } from "./geometry";

/** The reminder band that sits above a row's activities: a bell per reminder,
 *  placed at its date. A reminder is a moment to hit, not a span — so it reads
 *  as a point (bell), never a capsule. Upcoming ones are quiet in the project
 *  colour; a reminder that is past and still undone turns urgent (danger, a
 *  slow pulse). Done reminders are dropped — the timeline shows what is still
 *  owed, and a finished reminder is not a fact worth a permanent mark. */
export function ReminderLane({
  reminders,
  zoom,
  colorVar,
  width,
}: {
  reminders: Reminder[];
  zoom: ZoomLevel;
  colorVar: string;
  width: number;
}) {
  const today = todayISO();
  // Day columns are wide; centre the bell on the day rather than its left edge.
  const dayHalf = zoom === "day" ? ZOOM_CONFIG.day.colWidth / 2 : 0;
  const owed = reminders.filter((r) => !r.done);

  return (
    <div
      className="relative shrink-0 border-b border-[color:var(--dz-rem-band-border,rgba(120,150,190,.14))]"
      style={{ height: REMINDER_BAND, width, backgroundImage: rowBackgroundImage(zoom) }}
      aria-hidden
    >
      {owed.map((r) => {
        const date = r.remindAt.slice(0, 10);
        const overdue = date < today;
        const left = xForDate(date, zoom) + dayHalf;
        const repeats = r.repeat ? ` · repeats ${repeatPhrase(r.repeat)}` : "";
        return (
          <span
            key={r.id}
            className={cn(
              "absolute top-1/2 flex -translate-x-1/2 -translate-y-1/2 items-center justify-center",
              overdue ? "text-gtc-danger" : ""
            )}
            style={overdue ? { left } : { left, color: colorVar }}
            title={`${r.text} — ${date}${repeats}${overdue ? " · overdue" : ""}`}
          >
            <span className={cn("relative flex items-center justify-center", overdue && "animate-pulse")}>
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2">
                <path
                  d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6Z"
                  fill={overdue ? "color-mix(in srgb, currentColor 34%, transparent)" : "color-mix(in srgb, currentColor 18%, transparent)"}
                />
                <path d="M10 19.2a2 2 0 0 0 4 0" />
              </svg>
              {r.repeat && (
                <span
                  className="absolute -right-1 -top-1 h-[6px] w-[6px] rounded-full ring-1 ring-[color:var(--gtc-bg)]"
                  style={{ background: "currentColor" }}
                />
              )}
            </span>
          </span>
        );
      })}
    </div>
  );
}
