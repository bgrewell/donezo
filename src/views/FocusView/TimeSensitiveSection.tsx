import { Bell } from "lucide-react";
import { SectionLabel, cn } from "@grewelltech/console";

import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import type { DueRow } from "./useFocusData";
import { HAIRLINE_ROW } from "./shared";

/** TIME SENSITIVE — due/overdue tasks and upcoming reminders, soonest first. */
export function TimeSensitiveSection({ rows }: { rows: DueRow[] }) {
  return (
    <section>
      <SectionLabel className="mb-1 mt-0" trailing={<span>{rows.length}</span>}>
        Time sensitive
      </SectionLabel>
      {rows.length === 0 ? (
        <p className="py-1 font-sans text-[0.85rem] text-gtc-muted">
          Nothing pressing this week.
        </p>
      ) : (
        <ul>
          {rows.map((row) => (
            <li
              key={`${row.kind}-${row.id}`}
              className={cn(
                "flex items-center gap-3 py-2 transition-colors hover:bg-gtc-tint-accent",
                HAIRLINE_ROW
              )}
            >
              <span
                className={cn(
                  "inline-flex w-[100px] shrink-0 items-center justify-center rounded-gtc border px-1 py-0.5 font-mono text-[0.62rem] uppercase tracking-label",
                  row.overdue
                    ? "border-gtc-warn-dim text-gtc-warn"
                    : "border-gtc-line text-gtc-muted"
                )}
              >
                {row.overdue ? "needs review" : relativeFromToday(row.due)}
              </span>
              <span className="min-w-0 flex-1 truncate font-sans text-[0.85rem] text-gtc-text">
                {row.title}
              </span>
              {row.kind === "reminder" && (
                <Bell className="h-3.5 w-3.5 shrink-0 text-gtc-muted" aria-hidden />
              )}
              {row.project && (
                <span className="flex shrink-0 items-center gap-1.5">
                  <ProjectMark color={row.project.color} size={7} />
                  <span className="max-w-[180px] truncate font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted">
                    {row.project.name}
                  </span>
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
