import * as React from "react";
import { cn } from "@grewelltech/aether";

import type { ActivityEntry } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { formatDay } from "@/lib/time";
import { ActivityTypeIcon } from "@/components/common/activityTypes";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/Popover";
import { Tip } from "@/components/ui/Tooltip";

/** Popover listing a set of entries (day overflow, week/month aggregates).
 *  Selecting a row opens it in the inspector and closes the popover. */
export function EntriesPopover({
  heading,
  entries,
  tip,
  children,
}: {
  heading: string;
  entries: ActivityEntry[];
  /** Optional tooltip on the trigger (used by month/quarter density bars). */
  tip?: React.ReactNode;
  children: React.ReactElement;
}) {
  const [open, setOpen] = React.useState(false);
  const dispatch = useAppDispatch();
  const { selectedActivityId } = useAppState();

  const trigger = <PopoverTrigger asChild>{children}</PopoverTrigger>;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      {tip ? <Tip content={tip}>{trigger}</Tip> : trigger}
      <PopoverContent align="start" className="w-72 p-0">
        <div className="border-b border-ae-line px-2.5 py-1.5 font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
          {heading}
        </div>
        <div className="max-h-64 overflow-y-auto py-1">
          {entries.map((entry) => (
            <button
              key={entry.id}
              type="button"
              onClick={() => {
                dispatch({ type: "SELECT_ACTIVITY", id: entry.id });
                setOpen(false);
              }}
              className={cn(
                "flex w-full items-center gap-2 px-2.5 py-1.5 text-left outline-none transition-colors",
                "hover:bg-ae-tint-accent focus-visible:shadow-ae-focus",
                entry.planned && "opacity-60",
                selectedActivityId === entry.id && "bg-ae-tint-accent"
              )}
            >
              <ActivityTypeIcon
                type={entry.type}
                className={cn(
                  "h-3.5 w-3.5 shrink-0",
                  entry.type === "blocker" ? "text-ae-danger" : "text-ae-muted"
                )}
              />
              <span className="min-w-0 flex-1 truncate font-sans text-[0.78rem] text-ae-text">
                {entry.title}
              </span>
              <span className="shrink-0 font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
                {formatDay(entry.date)}
                {entry.effortHours ? ` · ${entry.effortHours}h` : ""}
                {entry.planned ? " · plan" : ""}
              </span>
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
