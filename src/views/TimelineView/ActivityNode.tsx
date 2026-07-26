import * as React from "react";
import { cn } from "@grewelltech/console";

import type { ActivityEntry } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { formatDay } from "@/lib/time";
import { ACTIVITY_TYPES, ActivityTypeIcon } from "@/components/common/activityTypes";
import { Tip } from "@/components/ui/Tooltip";
import { mixProject } from "./geometry";

/** Project-tinted surface recipe shared by nodes and aggregate capsules.
 *  Exposes hover tint via CSS vars so Tailwind classes stay static. */
export function nodeTintStyle(colorVar: string): React.CSSProperties {
  return {
    ["--dzn-bg" as string]: mixProject(colorVar, 16),
    ["--dzn-hv" as string]: mixProject(colorVar, 26),
    borderColor: mixProject(colorVar, 45),
  } as React.CSSProperties;
}

/** Static class half of the tint recipe (pairs with nodeTintStyle). */
export const nodeTintClass = "bg-[var(--dzn-bg)] hover:bg-[var(--dzn-hv)]";

/** One day-zoom activity capsule. Click selects it into the inspector. */
export function ActivityNode({
  entry,
  colorVar,
  style,
}: {
  entry: ActivityEntry;
  colorVar: string;
  style: React.CSSProperties;
}) {
  const dispatch = useAppDispatch();
  const { selectedActivityId } = useAppState();
  const selected = selectedActivityId === entry.id;
  const milestone = entry.type === "milestone";

  const tint = nodeTintStyle(colorVar);
  if (milestone) tint.borderColor = mixProject(colorVar, 85);

  return (
    <Tip
      content={
        <span className="flex flex-col gap-0.5">
          <span className="font-sans text-[0.8rem] text-gtc-text">{entry.title}</span>
          <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            {ACTIVITY_TYPES[entry.type].label}
            {entry.effortHours ? ` · ${entry.effortHours}h` : ""} · {formatDay(entry.date)}
            {entry.planned ? " · planned" : ""}
          </span>
        </span>
      }
    >
      <button
        type="button"
        onClick={() => dispatch({ type: "SELECT_ACTIVITY", id: entry.id })}
        className={cn(
          "absolute flex items-center gap-1 overflow-hidden rounded-gtc border px-1 text-left text-gtc-text",
          nodeTintClass,
          "outline-none transition-colors",
          entry.planned && "border-dashed opacity-[var(--dz-planned-opacity)]",
          milestone && "font-medium",
          selected && "shadow-[inset_0_0_0_1px_var(--gtc-accent)]",
          "focus-visible:shadow-gtc-focus"
        )}
        style={{ ...style, ...tint }}
      >
        {entry.type === "blocker" && (
          <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-gtc-danger" />
        )}
        <ActivityTypeIcon
          type={entry.type}
          className={cn(
            "h-3 w-3 shrink-0",
            entry.type === "blocker" ? "text-gtc-danger" : "text-gtc-muted"
          )}
        />
        <span className="min-w-0 flex-1 truncate font-sans text-[0.7rem]">
          {entry.title}
        </span>
      </button>
    </Tip>
  );
}
