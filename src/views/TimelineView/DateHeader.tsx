import { cn } from "@grewelltech/aether";

import type { ZoomLevel } from "@/domain/types";
import { isToday } from "@/lib/time";
import {
  GRID_LINE,
  WEEKEND_WASH,
  ZOOM_CONFIG,
  columns,
  quarterBands,
  railWidth,
  totalWidth,
} from "./geometry";

/** Sticky date header: corner cell + one labelled cell per column.
 *  Lives inside the single scroll container so alignment can never drift. */
export function DateHeader({
  zoom,
  railCollapsed,
  projectCount,
}: {
  zoom: ZoomLevel;
  railCollapsed: boolean;
  projectCount: number;
}) {
  const cfg = ZOOM_CONFIG[zoom];
  const cols = columns(zoom);

  return (
    <div
      className="sticky top-0 z-30 flex border-b border-ae-line bg-ae-panel"
      style={{ height: cfg.headerHeight }}
    >
      {/* Corner cell — sticky on both axes. */}
      <div
        className={cn(
          "sticky left-0 z-40 flex shrink-0 items-center border-r border-ae-line bg-ae-panel",
          railCollapsed ? "justify-center" : "justify-between px-2.5"
        )}
        style={{ width: railWidth(railCollapsed) }}
      >
        {!railCollapsed && (
          <span className="font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
            Projects
          </span>
        )}
        <span className="font-mono text-[0.68rem] text-ae-text">{projectCount}</span>
      </div>

      <div className="flex flex-col" style={{ width: totalWidth(zoom) }}>
        {zoom === "quarter" && (
          <div className="flex h-5" style={{ borderBottom: `1px solid ${GRID_LINE}` }}>
            {quarterBands().map((band) => (
              <div
                key={band.label}
                className="flex shrink-0 items-center px-2 font-mono text-[0.62rem] uppercase tracking-label text-ae-text"
                style={{
                  width: band.monthSpan * cfg.colWidth,
                  borderLeft: `1px solid ${GRID_LINE}`,
                }}
              >
                {band.label}
              </div>
            ))}
          </div>
        )}

        <div className="flex min-h-0 flex-1">
          {cols.map((col) => {
            const today = zoom === "day" && isToday(col.startISO);
            return (
              <div
                key={col.startISO}
                className="relative flex shrink-0 flex-col justify-center gap-px px-2"
                style={{
                  width: cfg.colWidth,
                  borderLeft: `1px solid ${GRID_LINE}`,
                  background: col.isWeekend ? WEEKEND_WASH : undefined,
                }}
              >
                {zoom === "quarter" ? (
                  <span className="font-mono text-[0.64rem] uppercase tracking-label text-ae-muted">
                    {col.label1}
                  </span>
                ) : zoom === "month" ? (
                  <>
                    <span className="font-mono text-[0.7rem] uppercase tracking-chrome text-ae-text">
                      {col.label1}
                    </span>
                    <span className="font-mono text-[0.6rem] uppercase tracking-label text-ae-muted">
                      {col.label2}
                    </span>
                  </>
                ) : (
                  <>
                    <span
                      className={cn(
                        "font-mono text-[0.6rem] uppercase tracking-label",
                        today ? "text-ae-accent" : "text-ae-muted"
                      )}
                    >
                      {col.label1}
                    </span>
                    <span
                      className={cn(
                        "font-mono text-[0.7rem] uppercase",
                        today ? "text-ae-accent" : "text-ae-text"
                      )}
                    >
                      {col.label2}
                    </span>
                  </>
                )}
                {today && (
                  <span
                    aria-hidden
                    className="absolute inset-x-0 bottom-0 h-0.5 bg-ae-accent shadow-ae-glow"
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
