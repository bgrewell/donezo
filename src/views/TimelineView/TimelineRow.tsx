import * as React from "react";
import { Flag } from "lucide-react";
import { cn } from "@grewelltech/console";
import { format, getISOWeek } from "date-fns";

import type { ActivityEntry, Project, ZoomLevel } from "@/domain/types";
import { diffDays, formatDay, parseDate, startOfMonthISO, startOfWeekISO } from "@/lib/time";
import { projectColorVar } from "@/lib/projectColors";
import {
  DAY_NODE_GAP,
  DAY_NODE_HEIGHT,
  DAY_NODE_INSET,
  MONTH_BAR_HEIGHT,
  RANGE_START,
  ROW_BORDER,
  WEEK_CAPSULE_HEIGHT,
  ZOOM_CONFIG,
  columns,
  dateAtX,
  rowBackgroundImage,
  totalWidth,
  xForDate,
} from "./geometry";
import { ProjectRailCell } from "./ProjectRailCell";
import { ActivityNode } from "./ActivityNode";
import { WeekCapsule, MonthBar, effortLabel } from "./AggregateCapsule";
import { EntriesPopover } from "./EntriesPopover";

/** Pending click-to-create state (coordinates are row-surface relative). */
export interface CreateDraft {
  projectId: string;
  dateISO: string;
  rowIndex: number;
  xInRow: number;
  yInRow: number;
}

function groupBy(
  entries: ActivityEntry[],
  key: (e: ActivityEntry) => string
): Map<string, ActivityEntry[]> {
  const map = new Map<string, ActivityEntry[]>();
  for (const e of entries) {
    const k = key(e);
    const list = map.get(k);
    if (list) list.push(e);
    else map.set(k, [e]);
  }
  return map;
}

/** One project row: sticky rail cell + the zoom-specific timeline lane. */
export function TimelineRow({
  project,
  entries,
  zoom,
  index,
  railCollapsed,
  onCreate,
}: {
  project: Project;
  entries: ActivityEntry[];
  zoom: ZoomLevel;
  index: number;
  railCollapsed: boolean;
  onCreate: (draft: CreateDraft) => void;
}) {
  const cfg = ZOOM_CONFIG[zoom];
  const width = totalWidth(zoom);
  const colorVar = projectColorVar(project.color);

  const handleSurfaceClick = (e: React.MouseEvent<HTMLDivElement>) => {
    // Only truly empty surface — clicks on nodes/capsules are theirs.
    if (e.target !== e.currentTarget) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    onCreate({
      projectId: project.id,
      dateISO: dateAtX(x, zoom),
      rowIndex: index,
      xInRow: x,
      yInRow: y,
    });
  };

  return (
    <div
      className="flex"
      style={{ height: cfg.rowHeight, borderBottom: `1px solid ${ROW_BORDER}` }}
    >
      <ProjectRailCell
        project={project}
        railCollapsed={railCollapsed}
        showFocus={zoom === "day" || zoom === "week"}
      />
      <div
        className={cn("relative shrink-0", index % 2 === 0 && "bg-white/[0.015]")}
        style={{ width, backgroundImage: rowBackgroundImage(zoom) }}
        onClick={handleSurfaceClick}
      >
        {zoom === "day" ? (
          <DayLane entries={entries} colorVar={colorVar} />
        ) : zoom === "week" ? (
          <WeekLane entries={entries} colorVar={colorVar} rowHeight={cfg.rowHeight} />
        ) : (
          <MonthLane
            entries={entries}
            colorVar={colorVar}
            rowHeight={cfg.rowHeight}
            zoom={zoom}
          />
        )}
      </div>
    </div>
  );
}

const chipCls =
  "absolute inline-flex items-center rounded-gtc border border-gtc-line bg-gtc-panel px-1.5 font-mono text-[0.62rem] text-gtc-muted outline-none transition-colors hover:border-gtc-accent-dim hover:text-gtc-text focus-visible:shadow-gtc-focus";

/** Non-planned entries first so real activity sits atop each day's stack. */
function sortDayEntries(list: ActivityEntry[]): ActivityEntry[] {
  return [...list].sort(
    (a, b) => Number(a.planned ?? false) - Number(b.planned ?? false)
  );
}

function DayLane({
  entries,
  colorVar,
}: {
  entries: ActivityEntry[];
  colorVar: string;
}) {
  const cfg = ZOOM_CONFIG.day;
  const colCount = columns("day").length;

  const groups = React.useMemo(() => groupBy(entries, (e) => e.date), [entries]);
  const dates = React.useMemo(() => [...groups.keys()].sort(), [groups]);

  return (
    <>
      {dates.map((date) => {
        const col = diffDays(date, RANGE_START);
        if (col < 0 || col >= colCount) return null;
        const x = col * cfg.colWidth;
        const list = sortDayEntries(groups.get(date) ?? []);
        const shown = list.length <= 3 ? list : list.slice(0, 2);
        return (
          <React.Fragment key={date}>
            {shown.map((entry, k) => (
              <ActivityNode
                key={entry.id}
                entry={entry}
                colorVar={colorVar}
                style={{
                  left: x + DAY_NODE_INSET,
                  top: DAY_NODE_GAP + k * (DAY_NODE_HEIGHT + DAY_NODE_GAP),
                  width: cfg.colWidth - DAY_NODE_INSET * 2,
                  height: DAY_NODE_HEIGHT,
                }}
              />
            ))}
            {list.length > 3 && (
              <EntriesPopover
                heading={`${formatDay(date)} · ${list.length} entries`}
                entries={list}
              >
                <button
                  type="button"
                  className={chipCls}
                  style={{
                    left: x + DAY_NODE_INSET,
                    top: DAY_NODE_GAP + 2 * (DAY_NODE_HEIGHT + DAY_NODE_GAP),
                    height: DAY_NODE_HEIGHT,
                  }}
                >
                  +{list.length - 2}
                </button>
              </EntriesPopover>
            )}
          </React.Fragment>
        );
      })}
    </>
  );
}

function WeekLane({
  entries,
  colorVar,
  rowHeight,
}: {
  entries: ActivityEntry[];
  colorVar: string;
  rowHeight: number;
}) {
  const cfg = ZOOM_CONFIG.week;
  const colCount = columns("week").length;

  const groups = React.useMemo(
    () => groupBy(entries, (e) => startOfWeekISO(e.date)),
    [entries]
  );
  const weeks = React.useMemo(() => [...groups.keys()].sort(), [groups]);

  return (
    <>
      {weeks.map((week) => {
        const col = diffDays(week, RANGE_START) / 7;
        if (col < 0 || col >= colCount) return null;
        const list = groups.get(week) ?? [];
        const effort = effortLabel(list);
        const heading = `WK ${getISOWeek(parseDate(week))} · ${list.length} entries${
          effort ? ` · ${effort}` : ""
        }`;
        return (
          <EntriesPopover key={week} heading={heading} entries={list}>
            <WeekCapsule
              entries={list}
              colorVar={colorVar}
              style={{
                left: col * cfg.colWidth + 4,
                top: (rowHeight - WEEK_CAPSULE_HEIGHT) / 2,
                width: cfg.colWidth - 8,
                height: WEEK_CAPSULE_HEIGHT,
              }}
            />
          </EntriesPopover>
        );
      })}
    </>
  );
}

function MonthLane({
  entries,
  colorVar,
  rowHeight,
  zoom,
}: {
  entries: ActivityEntry[];
  colorVar: string;
  rowHeight: number;
  zoom: "month" | "quarter";
}) {
  const cfg = ZOOM_CONFIG[zoom];
  const colCount = columns(zoom).length;

  const groups = React.useMemo(
    () => groupBy(entries, (e) => startOfMonthISO(e.date)),
    [entries]
  );
  const months = React.useMemo(() => [...groups.keys()].sort(), [groups]);
  const maxCount = React.useMemo(() => {
    let max = 0;
    for (const list of groups.values()) max = Math.max(max, list.length);
    return max;
  }, [groups]);

  const barTop = (rowHeight - MONTH_BAR_HEIGHT) / 2;

  return (
    <>
      {months.map((month) => {
        const first = groups.get(month)?.[0];
        if (!first) return null;
        const col = Math.floor(xForDate(month, zoom) / cfg.colWidth);
        if (col < 0 || col >= colCount) return null;
        const list = groups.get(month) ?? [];
        const monthName = format(parseDate(month), "MMMM");
        const effort = effortLabel(list);
        const tipLabel = `${list.length} entries${effort ? ` · ${effort}` : ""} · ${monthName}`;
        return (
          <React.Fragment key={month}>
            <EntriesPopover
              heading={`${format(parseDate(month), "MMMM yyyy")} · ${list.length} entries`}
              entries={list}
              tip={
                <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                  {tipLabel}
                </span>
              }
            >
              <MonthBar
                entries={list}
                colorVar={colorVar}
                x={col * cfg.colWidth}
                colWidth={cfg.colWidth}
                rowHeight={rowHeight}
                maxCount={maxCount}
              />
            </EntriesPopover>
            {list
              .filter((e) => e.type === "milestone")
              .map((m) => (
                <Flag
                  key={m.id}
                  aria-hidden
                  className="pointer-events-none absolute h-3 w-3"
                  style={{
                    left: xForDate(m.date, zoom) - 6,
                    top: barTop - 15,
                    color: colorVar,
                  }}
                />
              ))}
          </React.Fragment>
        );
      })}
    </>
  );
}
