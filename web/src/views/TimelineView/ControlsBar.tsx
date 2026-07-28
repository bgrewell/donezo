import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  PanelLeftClose,
  PanelLeftOpen,
} from "lucide-react";
import { Button, cn } from "@grewelltech/console";

import type { ActivityType, ZoomLevel } from "@/domain/types";
import { clampAnchor, shiftAnchor } from "@/lib/time";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { ProjectMark } from "@/components/common/ProjectMark";
import {
  ACTIVITY_TYPES,
  ACTIVITY_TYPE_IDS,
  ActivityTypeIcon,
} from "@/components/common/activityTypes";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/DropdownMenu";
import { visibleColumnCount, visibleRangeLabel } from "./geometry";

const ZOOM_LEVELS: { id: ZoomLevel; label: string }[] = [
  { id: "day", label: "Day" },
  { id: "week", label: "Week" },
  { id: "month", label: "Month" },
  { id: "quarter", label: "Quarter" },
];

const iconBtn =
  "flex h-7 w-7 shrink-0 items-center justify-center rounded-gtc text-gtc-muted outline-none transition-colors hover:bg-gtc-tint-accent hover:text-gtc-text focus-visible:shadow-gtc-focus";

const triggerBase =
  "inline-flex h-7 shrink-0 select-none items-center gap-1.5 rounded-gtc border px-2 font-mono text-[0.66rem] uppercase tracking-chrome outline-none transition-colors focus-visible:shadow-gtc-focus";
const triggerIdle =
  "border-gtc-line text-gtc-muted hover:border-gtc-accent-dim hover:text-gtc-text";
const triggerActive = "border-gtc-accent-dim bg-gtc-tint-accent text-gtc-accent";

/** Timeline controls: period nav, range label, zoom, filters, rail toggle. */
export function ControlsBar({ visibleWidth }: { visibleWidth: number }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { zoom, anchorDate, filters, railCollapsed, projects } = state;

  /** Whether a prev/next shift would actually move the anchor (walls). */
  const canShift = (dir: 1 | -1) => {
    const next = clampAnchor(shiftAnchor(anchorDate, zoom, dir), zoom);
    return dir === 1 ? next > anchorDate : next < anchorDate;
  };

  const toggleProject = (id: string, checked: boolean) => {
    const all = projects.map((p) => p.id);
    const current = filters.projectIds ?? all;
    const next = checked
      ? [...new Set([...current, id])]
      : current.filter((x) => x !== id);
    dispatch({
      type: "SET_FILTERS",
      patch: { projectIds: next.length === all.length ? null : next },
    });
  };

  const toggleType = (id: ActivityType, checked: boolean) => {
    const current = filters.types ?? ACTIVITY_TYPE_IDS;
    const next = checked
      ? ([...new Set([...current, id])] as ActivityType[])
      : current.filter((x) => x !== id);
    dispatch({
      type: "SET_FILTERS",
      patch: { types: next.length === ACTIVITY_TYPE_IDS.length ? null : next },
    });
  };

  return (
    <div className="flex h-9 shrink-0 items-center gap-1.5 overflow-x-auto border-b border-gtc-line bg-gtc-panel px-2">
      <button
        type="button"
        aria-label="Previous period"
        disabled={!canShift(-1)}
        className={cn(iconBtn, "disabled:pointer-events-none disabled:opacity-40")}
        onClick={() => dispatch({ type: "SHIFT_PERIOD", dir: -1 })}
      >
        <ChevronLeft className="h-4 w-4" aria-hidden />
      </button>
      <button
        type="button"
        aria-label="Next period"
        disabled={!canShift(1)}
        className={cn(iconBtn, "disabled:pointer-events-none disabled:opacity-40")}
        onClick={() => dispatch({ type: "SHIFT_PERIOD", dir: 1 })}
      >
        <ChevronRight className="h-4 w-4" aria-hidden />
      </button>
      <Button
        size="sm"
        className="shrink-0"
        onClick={() =>
          // The measured lane tells JUMP_TODAY how many columns fit, so the
          // jump keeps today on screen on narrow lanes (phones/tablets).
          dispatch({
            type: "JUMP_TODAY",
            visibleColumns: visibleColumnCount(visibleWidth, zoom),
          })
        }
      >
        Today
      </Button>

      <span className="hidden whitespace-nowrap px-1.5 font-mono text-[0.66rem] font-medium uppercase tracking-label text-gtc-title xl:inline">
        {visibleRangeLabel(anchorDate, zoom, visibleWidth)}
      </span>

      <div
        role="group"
        aria-label="Zoom level"
        data-tour="zoom"
        className="ml-1 inline-flex shrink-0 items-stretch divide-x divide-gtc-line rounded-gtc border border-gtc-line"
      >
        {ZOOM_LEVELS.map((z) => {
          const active = zoom === z.id;
          return (
            <button
              key={z.id}
              type="button"
              aria-pressed={active}
              onClick={() => dispatch({ type: "SET_ZOOM", zoom: z.id })}
              className={cn(
                "px-2.5 py-1 font-mono text-[0.68rem] uppercase tracking-chrome outline-none transition-colors focus-visible:shadow-gtc-focus",
                active
                  ? "bg-gtc-tint-accent text-gtc-accent"
                  : "text-gtc-muted hover:text-gtc-text"
              )}
            >
              {z.label}
            </button>
          );
        })}
      </div>

      <div className="ml-auto flex shrink-0 items-center gap-1.5 pl-1.5">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className={cn(triggerBase, filters.projectIds ? triggerActive : triggerIdle)}
            >
              Projects
              {filters.projectIds && (
                <span>{filters.projectIds.length}/{projects.length}</span>
              )}
              <ChevronDown className="h-3 w-3" aria-hidden />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Filter projects</DropdownMenuLabel>
            <DropdownMenuItem
              onSelect={(e) => {
                e.preventDefault();
                dispatch({ type: "SET_FILTERS", patch: { projectIds: null } });
              }}
            >
              All projects
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {projects.map((p) => (
              <DropdownMenuCheckboxItem
                key={p.id}
                checked={!filters.projectIds || filters.projectIds.includes(p.id)}
                onSelect={(e) => e.preventDefault()}
                onCheckedChange={(c) => toggleProject(p.id, c === true)}
              >
                <ProjectMark color={p.color} size={7} />
                {p.name}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className={cn(triggerBase, filters.types ? triggerActive : triggerIdle)}
            >
              Types
              {filters.types && (
                <span>{filters.types.length}/{ACTIVITY_TYPE_IDS.length}</span>
              )}
              <ChevronDown className="h-3 w-3" aria-hidden />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Filter types</DropdownMenuLabel>
            <DropdownMenuItem
              onSelect={(e) => {
                e.preventDefault();
                dispatch({ type: "SET_FILTERS", patch: { types: null } });
              }}
            >
              All types
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {ACTIVITY_TYPE_IDS.map((t) => (
              <DropdownMenuCheckboxItem
                key={t}
                checked={!filters.types || filters.types.includes(t)}
                onSelect={(e) => e.preventDefault()}
                onCheckedChange={(c) => toggleType(t, c === true)}
              >
                <ActivityTypeIcon type={t} className="h-3.5 w-3.5 text-gtc-muted" />
                {ACTIVITY_TYPES[t].label}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <button
          type="button"
          aria-pressed={filters.showPlanned}
          onClick={() =>
            dispatch({ type: "SET_FILTERS", patch: { showPlanned: !filters.showPlanned } })
          }
          className={cn(triggerBase, filters.showPlanned ? triggerActive : triggerIdle)}
        >
          Planned
        </button>
        <button
          type="button"
          aria-pressed={filters.showCompleted}
          onClick={() =>
            dispatch({
              type: "SET_FILTERS",
              patch: { showCompleted: !filters.showCompleted },
            })
          }
          className={cn(triggerBase, filters.showCompleted ? triggerActive : triggerIdle)}
        >
          Done
        </button>

        <button
          type="button"
          aria-label={railCollapsed ? "Expand project rail" : "Collapse project rail"}
          className={iconBtn}
          onClick={() => dispatch({ type: "TOGGLE_RAIL" })}
        >
          {railCollapsed ? (
            <PanelLeftOpen className="h-4 w-4" aria-hidden />
          ) : (
            <PanelLeftClose className="h-4 w-4" aria-hidden />
          )}
        </button>
      </div>
    </div>
  );
}
