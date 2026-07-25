import * as React from "react";

import type { ActivityEntry } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { filteredActivities, visibleProjects } from "@/state/selectors";
import { todayISO } from "@/lib/time";
import { EmptyState } from "@/components/common/EmptyState";

import { ControlsBar } from "./ControlsBar";
import { DateHeader } from "./DateHeader";
import { TimelineRow, type CreateDraft } from "./TimelineRow";
import { LogActivityPopover } from "./LogActivityPopover";
import {
  RANGE_END,
  RANGE_START,
  ZOOM_CONFIG,
  dateAtX,
  railWidth,
  totalWidth,
  xForDate,
} from "./geometry";

/** Project Pulse — the signature project-by-time visualization.
 *  One scroll container holds the date header and every project row, so
 *  horizontal and vertical alignment can never drift. */
export default function TimelineView() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { zoom, anchorDate, railCollapsed } = state;

  const projects = visibleProjects(state);
  const acts = filteredActivities(state);
  const byProject = React.useMemo(() => {
    const map = new Map<string, ActivityEntry[]>();
    for (const a of acts) {
      const list = map.get(a.projectId);
      if (list) list.push(a);
      else map.set(a.projectId, [a]);
    }
    for (const list of map.values()) list.sort((a, b) => a.date.localeCompare(b.date));
    return map;
  }, [acts]);

  const cfg = ZOOM_CONFIG[zoom];
  const width = totalWidth(zoom);
  const railW = railWidth(railCollapsed);
  const today = todayISO();
  const todayInRange = today >= RANGE_START && today <= RANGE_END;
  const todayX = xForDate(today, zoom);

  // --- scroll <-> anchor -------------------------------------------------
  const scrollerRef = React.useRef<HTMLDivElement>(null);
  const zoomRef = React.useRef(zoom);
  zoomRef.current = zoom;
  const anchorRef = React.useRef(anchorDate);
  anchorRef.current = anchorDate;
  const firstPaint = React.useRef(true);
  /** Target of an in-flight programmatic scroll (user scrolls are ignored). */
  const pendingTarget = React.useRef<number | null>(null);
  /** Anchor value we just dispatched from a user scroll — skip re-scrolling. */
  const scrollDispatched = React.useRef<string | null>(null);
  const debounceTimer = React.useRef<number | undefined>(undefined);

  React.useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    if (scrollDispatched.current === anchorDate) {
      scrollDispatched.current = null;
      firstPaint.current = false;
      return;
    }
    const max = Math.max(0, el.scrollWidth - el.clientWidth);
    const target = Math.min(Math.max(0, xForDate(anchorDate, zoom)), max);
    if (Math.abs(el.scrollLeft - target) < 2) {
      firstPaint.current = false;
      return;
    }
    pendingTarget.current = target;
    el.scrollTo({ left: target, behavior: firstPaint.current ? "auto" : "smooth" });
    firstPaint.current = false;
    // Safety valve in case the smooth scroll is interrupted mid-flight.
    const timeout = window.setTimeout(() => {
      pendingTarget.current = null;
    }, 1500);
    return () => window.clearTimeout(timeout);
  }, [anchorDate, zoom]);

  React.useEffect(() => () => window.clearTimeout(debounceTimer.current), []);

  const handleScroll = () => {
    const el = scrollerRef.current;
    if (!el) return;
    if (pendingTarget.current !== null) {
      if (Math.abs(el.scrollLeft - pendingTarget.current) < 2) {
        pendingTarget.current = null;
      }
      return;
    }
    window.clearTimeout(debounceTimer.current);
    debounceTimer.current = window.setTimeout(() => {
      const scroller = scrollerRef.current;
      if (!scroller || pendingTarget.current !== null) return;
      const date = dateAtX(scroller.scrollLeft, zoomRef.current);
      if (date !== anchorRef.current) {
        scrollDispatched.current = date;
        dispatch({ type: "SET_ANCHOR", date });
      }
    }, 150);
  };

  // --- click-to-create ---------------------------------------------------
  const [draft, setDraft] = React.useState<CreateDraft | null>(null);
  React.useEffect(() => setDraft(null), [zoom]);
  const draftProject = draft
    ? projects.find((p) => p.id === draft.projectId)
    : undefined;

  return (
    <div className="flex h-full flex-col">
      <ControlsBar />
      <div
        ref={scrollerRef}
        onScroll={handleScroll}
        className="min-h-0 flex-1 overflow-auto"
      >
        <div className="relative" style={{ width: `calc(${railW} + ${width}px)` }}>
          <DateHeader
            zoom={zoom}
            railCollapsed={railCollapsed}
            projectCount={projects.length}
          />
          {projects.map((project, index) => (
            <TimelineRow
              key={project.id}
              project={project}
              index={index}
              zoom={zoom}
              entries={byProject.get(project.id) ?? []}
              railCollapsed={railCollapsed}
              onCreate={setDraft}
            />
          ))}
          {projects.length === 0 && (
            <div className="sticky left-0 w-[440px] p-6">
              <EmptyState
                title="No projects visible"
                hint="Loosen the project filter, or show completed projects with Done."
              />
            </div>
          )}
          {todayInRange && projects.length > 0 && (
            <div
              aria-hidden
              className="pointer-events-none absolute z-10 w-px"
              style={{
                left: `calc(${railW} + ${todayX}px)`,
                top: cfg.headerHeight,
                height: projects.length * cfg.rowHeight,
                background: "var(--ae-accent)",
                opacity: 0.55,
              }}
            />
          )}
          {draft && draftProject && (
            <LogActivityPopover
              key={`${draft.projectId}-${draft.dateISO}-${draft.xInRow}-${draft.yInRow}`}
              draft={draft}
              project={draftProject}
              railWidthExpr={railW}
              headerHeight={cfg.headerHeight}
              rowHeight={cfg.rowHeight}
              onClose={() => setDraft(null)}
            />
          )}
        </div>
      </div>
    </div>
  );
}
