import * as React from "react";
import { Flag, OctagonAlert } from "lucide-react";
import { cn } from "@grewelltech/aether";

import type { ActivityEntry } from "@/domain/types";
import { MONTH_BAR_HEIGHT, mixProject } from "./geometry";
import { nodeTintClass, nodeTintStyle } from "./ActivityNode";

/** Sum of rough effort hours across entries. */
export function totalEffort(entries: ActivityEntry[]): number {
  return entries.reduce((sum, e) => sum + (e.effortHours ?? 0), 0);
}

/** "~14h" label, or null when no effort was recorded. */
export function effortLabel(entries: ActivityEntry[]): string | null {
  const h = totalEffort(entries);
  return h > 0 ? `~${Math.round(h)}h` : null;
}

export interface WeekCapsuleProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  entries: ActivityEntry[];
  colorVar: string;
}

/** Week-zoom aggregate: count, effort, a short summary, micro-icons.
 *  forwardRef + prop spread so it can be a Radix popover trigger. */
export const WeekCapsule = React.forwardRef<HTMLButtonElement, WeekCapsuleProps>(
  function WeekCapsule({ entries, colorVar, className, style, ...rest }, ref) {
    const milestone = entries.find((e) => e.type === "milestone");
    const hasBlocker = entries.some((e) => e.type === "blocker");
    const allPlanned = entries.length > 0 && entries.every((e) => e.planned);
    const effort = effortLabel(entries);
    const summary = milestone?.title ?? entries[0]?.title ?? "";

    return (
      <button
        ref={ref}
        type="button"
        {...rest}
        className={cn(
          "absolute flex items-center gap-1.5 overflow-hidden rounded-ae border px-2 text-left",
          nodeTintClass,
          "outline-none transition-colors focus-visible:shadow-ae-focus",
          allPlanned && "border-dashed opacity-60",
          className
        )}
        style={{ ...style, ...nodeTintStyle(colorVar) }}
      >
        <span className="shrink-0 font-mono text-[0.66rem] text-ae-text">
          {entries.length}
        </span>
        {effort && (
          <span className="shrink-0 font-mono text-[0.62rem] text-ae-muted">{effort}</span>
        )}
        <span className="min-w-0 flex-1 truncate font-sans text-[0.72rem] text-ae-text">
          {summary}
        </span>
        {milestone && (
          <Flag className="h-3 w-3 shrink-0" style={{ color: colorVar }} aria-hidden />
        )}
        {hasBlocker && (
          <OctagonAlert className="h-3 w-3 shrink-0 text-ae-danger" aria-hidden />
        )}
      </button>
    );
  }
);

export interface MonthBarProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  entries: ActivityEntry[];
  colorVar: string;
  /** Column left edge, px. */
  x: number;
  colWidth: number;
  rowHeight: number;
  /** Entry count of this project's busiest month (width/intensity scale). */
  maxCount: number;
}

/** Month/quarter density bar: width and tint scale with entry count. */
export const MonthBar = React.forwardRef<HTMLButtonElement, MonthBarProps>(
  function MonthBar(
    { entries, colorVar, x, colWidth, rowHeight, maxCount, className, style, ...rest },
    ref
  ) {
    const ratio = maxCount > 0 ? entries.length / maxCount : 0;
    const maxW = colWidth - 16;
    const width = Math.max(14, Math.min(maxW, Math.round(ratio * maxW)));
    const tintPct = 30 + Math.round(35 * ratio);
    const allPlanned = entries.length > 0 && entries.every((e) => e.planned);

    return (
      <button
        ref={ref}
        type="button"
        aria-label={`${entries.length} entries`}
        {...rest}
        className={cn(
          "absolute rounded-ae border outline-none transition-colors focus-visible:shadow-ae-focus",
          allPlanned && "border-dashed opacity-60",
          className
        )}
        style={{
          left: x + (colWidth - width) / 2,
          top: (rowHeight - MONTH_BAR_HEIGHT) / 2,
          width,
          height: MONTH_BAR_HEIGHT,
          background: mixProject(colorVar, tintPct),
          borderColor: mixProject(colorVar, 50),
          ...style,
        }}
      />
    );
  }
);
