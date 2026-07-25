import * as React from "react";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { parseHash } from "@/lib/route";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { NavRail } from "./NavRail";
import { TopBar } from "./TopBar";
import { Inspector } from "./Inspector";
import { QuickCapture } from "./QuickCapture";

/** Application chrome: nav rail, top bar, workspace, inspector, quick capture.
 *  Also owns hash routing and global keyboard shortcuts. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { view, selectedProjectId, selectedActivityId, quickCaptureOpen } = state;

  // Hash → state for manual URL edits after load. (The initial hash seeds
  // AppStore.initialState directly — syncing it here raced under StrictMode.)
  React.useEffect(() => {
    const apply = () => {
      const parsed = parseHash(window.location.hash);
      if (!parsed) return;
      if (parsed.view === "projects" && parsed.projectId) {
        dispatch({ type: "OPEN_PROJECT", projectId: parsed.projectId });
      } else {
        dispatch({ type: "SET_VIEW", view: parsed.view });
      }
    };
    window.addEventListener("hashchange", apply);
    return () => window.removeEventListener("hashchange", apply);
  }, [dispatch]);

  // State → hash (bookmarkable views; replaceState avoids history spam).
  React.useEffect(() => {
    const desired =
      view === "projects" && selectedProjectId
        ? `#/projects/${selectedProjectId}`
        : `#/${view}`;
    if (window.location.hash !== desired) {
      window.history.replaceState(null, "", desired);
    }
  }, [view, selectedProjectId]);

  // Global shortcuts: Cmd/Ctrl+K quick capture, Escape closes the inspector.
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        dispatch({ type: "SET_QUICK_CAPTURE", open: !quickCaptureOpen });
        return;
      }
      // Radix layers call preventDefault for Escape they consume.
      if (e.key === "Escape" && !e.defaultPrevented && !quickCaptureOpen && selectedActivityId) {
        dispatch({ type: "SELECT_ACTIVITY", id: null });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dispatch, quickCaptureOpen, selectedActivityId]);

  return (
    <TooltipProvider>
      <div className="flex h-full bg-gtc-page text-gtc-text">
        <NavRail />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar />
          <main className="min-h-0 flex-1 overflow-hidden">{children}</main>
        </div>
        <Inspector />
      </div>
      <QuickCapture />
    </TooltipProvider>
  );
}
