import * as React from "react";
import { Bell } from "lucide-react";
import { SectionLabel, cn } from "@grewelltech/console";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { relativeFromToday } from "@/lib/time";
import { describeRepeat, repeatPhrase } from "@/lib/repeat";
import { ProjectMark } from "@/components/common/ProjectMark";
import { RowActions } from "@/components/common/RowActions";
import { DetailsDisclosure } from "@/components/common/DetailsDisclosure";
import { ReminderEditor } from "@/components/common/ReminderEditor";
import { TaskEditor } from "@/components/common/TaskEditor";
import type { DueRow } from "./useFocusData";
import { HAIRLINE_ROW } from "./shared";

/** TIME SENSITIVE — due/overdue tasks and upcoming reminders, soonest first.
 *
 *  This was #29's clearest example: the rows told you what needed attention
 *  and gave you nowhere to give it, so acting meant finding the same item
 *  again somewhere it happened to be editable. Each row now completes, edits
 *  or opens its project in place — revealed on hover, so the section still
 *  reads as something to look at rather than a control panel. */
export function TimeSensitiveSection({ rows }: { rows: DueRow[] }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const [editing, setEditing] = React.useState<string | null>(null);

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
          {rows.map((row) => {
            const key = `${row.kind}-${row.id}`;
            const task = row.kind === "task" ? state.tasks.find((t) => t.id === row.id) : undefined;
            const reminder =
              row.kind === "reminder" ? state.reminders.find((r) => r.id === row.id) : undefined;
            const details = task?.details ?? reminder?.details ?? "";

            if (editing === key) {
              return (
                <li key={key} className={cn("py-2", HAIRLINE_ROW)}>
                  {task && <TaskEditor task={task} onDone={() => setEditing(null)} />}
                  {reminder && (
                    <ReminderEditor reminder={reminder} onDone={() => setEditing(null)} />
                  )}
                </li>
              );
            }

            return (
            <li
              key={key}
              className={cn(
                "group relative flex flex-wrap items-center gap-x-3 gap-y-1 py-2 transition-colors hover:bg-gtc-tint-accent",
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
                <span
                  className="inline-flex shrink-0 items-center gap-1 text-gtc-muted"
                  title={reminder?.repeat ? describeRepeat(reminder.repeat) : undefined}
                >
                  <Bell className="h-3.5 w-3.5" aria-hidden />
                  {reminder?.repeat && (
                    <span className="font-mono text-[0.62rem] uppercase tracking-label">
                      {repeatPhrase(reminder.repeat)}
                    </span>
                  )}
                </span>
              )}
              {row.project && (
                // Drops under the title on phones instead of crushing it.
                <span className="flex shrink-0 basis-full items-center gap-1.5 sm:basis-auto">
                  <ProjectMark color={row.project.color} size={7} />
                  <span className="max-w-[180px] truncate font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted">
                    {row.project.name}
                  </span>
                </span>
              )}
              <RowActions
                label={`Actions for ${row.title}`}
                actions={[
                  {
                    label: "Done",
                    onSelect: () => {
                      if (row.kind === "task") {
                        dispatch({ type: "UPDATE_TASK", id: row.id, patch: { status: "done" } });
                        // One commitment can exist as both a task and a
                        // reminder; this row hid the reminder, so finishing
                        // the task has to finish it too. Otherwise the
                        // reminder takes the row back and Done looks like it
                        // did nothing.
                        if (row.mirrors) {
                          dispatch({ type: "UPDATE_REMINDER", id: row.mirrors, patch: { done: true } });
                        }
                      } else {
                        dispatch({ type: "UPDATE_REMINDER", id: row.id, patch: { done: true } });
                      }
                    },
                  },
                  { label: "Edit", onSelect: () => setEditing(key) },
                  ...(row.project
                    ? [
                        {
                          label: "Open project",
                          onSelect: () =>
                            dispatch({ type: "OPEN_PROJECT", projectId: row.project!.id }),
                        },
                      ]
                    : []),
                ]}
              />
              {details && (
                <span className="basis-full">
                  <DetailsDisclosure details={details} />
                </span>
              )}
            </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
