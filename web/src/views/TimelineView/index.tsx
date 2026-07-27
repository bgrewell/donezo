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
  rowBackgroundImage,
  totalWidth,
  xForDate,
} from "./geometry";

/** After a programmatic scroll settles, ignore scroll-derived re-anchoring
 *  for this long — the settling frames must not overwrite the anchor. */
const ANCHOR_GRACE_MS = 300;

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
  /** Ignore scroll-derived anchors until this timestamp (settle frames and
   *  the browser's own clamp-scroll after a zoom shrinks the track). */
  const graceUntil = React.useRef(0);
  const debounceTimer = React.useRef<number | undefined>(undefined);

  // Visible timeline lane width (scroller minus the sticky rail) — drives the
  // honest range label in the controls bar.
  const [visibleWidth, setVisibleWidth] = React.useState(1160);
  React.useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const railPx = railCollapsed
      ? 44
      : parseFloat(getComputedStyle(el).getPropertyValue("--dz-rail-w")) || 260;
    const measure = () => setVisibleWidth(Math.max(0, el.clientWidth - railPx));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [railCollapsed]);

  React.useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    if (scrollDispatched.current === anchorDate) {
      scrollDispatched.current = null;
      firstPaint.current = false;
      return;
    }
    const max = Math.max(0, el.scrollWidth - el.clientWidth);
    // Snap to a column start (after the max-scroll clamp) so the leftmost
    // period is never clipped mid-column behind the rail.
    const w = ZOOM_CONFIG[zoom].colWidth;
    const target =
      Math.floor(Math.min(Math.max(0, xForDate(anchorDate, zoom)), max) / w) * w;
    if (Math.abs(el.scrollLeft - target) < 2) {
      // Already in place — but a zoom switch may still emit a clamp-scroll.
      graceUntil.current = Date.now() + ANCHOR_GRACE_MS;
      firstPaint.current = false;
      return;
    }
    pendingTarget.current = target;
    el.scrollTo({ left: target, behavior: firstPaint.current ? "auto" : "smooth" });
    firstPaint.current = false;
    // Safety valve in case the smooth scroll is interrupted mid-flight.
    const timeout = window.setTimeout(() => {
      if (pendingTarget.current !== null) {
        pendingTarget.current = null;
        graceUntil.current = Date.now() + ANCHOR_GRACE_MS;
      }
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
        graceUntil.current = Date.now() + ANCHOR_GRACE_MS;
      }
      return;
    }
    window.clearTimeout(debounceTimer.current);
    debounceTimer.current = window.setTimeout(() => {
      const scroller = scrollerRef.current;
      if (!scroller || pendingTarget.current !== null) return;
      if (Date.now() < graceUntil.current) return;
      const zoomNow = zoomRef.current;
      const derived = dateAtX(scroller.scrollLeft, zoomNow);
      // Sub-column drift keeps the fine-grained anchor; only re-anchor once
      // the left edge has left the anchor's own column.
      const anchorCol = dateAtX(xForDate(anchorRef.current, zoomNow), zoomNow);
      if (derived === anchorCol || derived === anchorRef.current) return;
      scrollDispatched.current = derived;
      dispatch({ type: "SET_ANCHOR", date: derived });
    }, 150);
  };

  // --- click-to-create ---------------------------------------------------
  const [draft, setDraft] = React.useState<CreateDraft | null>(null);
  React.useEffect(() => setDraft(null), [zoom]);
  const draftProject = draft
    ? projects.find((p) => p.id === draft.projectId)
    : undefined;

  return (
    <div className="relative flex h-full flex-col">
      <ControlsBar visibleWidth={visibleWidth} />
      {/* Tour anchor while the rail is collapsed: the expanded rail cells
          (data-tour="rail") don't exist, and a single 44px cell is too small
          a spotlight — so target the whole rail column. Inert; sits outside
          the scroller so it never scrolls away. top-9 clears the controls
          bar (h-9). */}
      {railCollapsed && projects.length > 0 && (
        <div
          aria-hidden
          data-tour="rail"
          className="pointer-events-none absolute bottom-0 left-0 top-9"
          style={{ width: railW }}
        />
      )}
      <div
        ref={scrollerRef}
        onScroll={handleScroll}
        data-tour="pulse"
        className="min-h-0 flex-1 overflow-auto"
      >
        <div
          className="relative flex min-h-full flex-col"
          style={{ width: `calc(${railW} + ${width}px)` }}
        >
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
              >
                {/* A truly fresh space has no projects to unhide — point
                    newcomers at the space switcher instead. */}
                {state.projects.length === 0 && (
                  <p className="m-0 max-w-[42ch] text-[0.85rem] text-gtc-muted">
                    New here? Spaces live under the donezo mark, top-left.
                  </p>
                )}
              </EmptyState>
            </div>
          )}
          {/* Filler lane: the column grid (and weekend wash) continues below
              the last row so the lower workspace reads as canvas. */}
          <div aria-hidden className="pointer-events-none flex min-h-0 flex-1">
            <div
              className="sticky left-0 z-20 shrink-0 border-r border-gtc-line bg-gtc-panel"
              style={{ width: railW }}
            />
            <div
              className="shrink-0"
              style={{ width, backgroundImage: rowBackgroundImage(zoom) }}
            />
          </div>
          {todayInRange && projects.length > 0 && (
            <>
              <div
                aria-hidden
                className="pointer-events-none absolute z-10 w-px"
                style={{
                  left: `calc(${railW} + ${todayX}px)`,
                  top: cfg.headerHeight,
                  bottom: 0,
                  background: "var(--gtc-accent)",
                  opacity: 0.55,
                }}
              />
              {/* Tour anchor: today's column. The "Log as it happens" step
                  spotlights one concrete place to click instead of the whole
                  pane (cells are painted grid, not elements). Inert. */}
              <div
                aria-hidden
                data-tour="log"
                className="pointer-events-none absolute"
                style={{
                  left: `calc(${railW} + ${todayX}px)`,
                  width: cfg.colWidth,
                  top: cfg.headerHeight,
                  bottom: 0,
                }}
              />
            </>
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
