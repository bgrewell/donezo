import * as React from "react";

import { useAppDispatch, useAppState, useSpaceId } from "@/state/AppStore";
import { useAppearanceSync } from "@/state/useAppearanceSync";
import { useSpaceFreshness } from "@/state/useSpaceFreshness";
import { useTimezoneSync } from "@/state/useTimezoneSync";
import { parseHash } from "@/lib/route";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { NavRail } from "./NavRail";
import { TopBar } from "./TopBar";
import { SyncErrorBanner } from "./SyncErrorBanner";
import { Inspector } from "./Inspector";
import { QuickCapture } from "./QuickCapture";
import { WelcomeDialog } from "@/components/onboarding/WelcomeDialog";
import { Tour } from "@/components/onboarding/Tour";
import { HintChip } from "@/components/onboarding/HintChip";

/** Application chrome: nav rail, top bar, workspace, inspector, quick capture.
 *  Also owns hash routing and global keyboard shortcuts. */
export function AppShell({ children }: { children: React.ReactNode }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { view, selectedProjectId, selectedActivityId, quickCaptureOpen, settingsSection } =
    state;
  // Appearance preferences follow the user between machines. Mounted here
  // rather than in ThemeProvider because the provider sits above the auth
  // gate, and an anonymous request has no settings to read.
  useAppearanceSync();
  // Tell the server which zone this browser is in, so a write that arrives
  // without one — an agent over MCP — is dated the same day this browser
  // would date it.
  useTimezoneSync();
  // Keep the store current as the server changes — another tab, another
  // machine, or an agent writing over MCP. Mounted here for the same reason
  // as appearance: it needs an authenticated session.
  useSpaceFreshness(useSpaceId());

  // Hash → state for manual URL edits after load. (The initial hash seeds
  // AppStore.initialState directly — syncing it here raced under StrictMode.)
  React.useEffect(() => {
    const apply = () => {
      const parsed = parseHash(window.location.hash);
      if (!parsed) return;
      if (parsed.view === "projects" && parsed.projectId) {
        dispatch({ type: "OPEN_PROJECT", projectId: parsed.projectId });
      } else if (parsed.view === "settings" && parsed.settingsSection) {
        dispatch({ type: "SET_SETTINGS_SECTION", section: parsed.settingsSection });
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
        : view === "settings" && settingsSection
          ? `#/settings/${settingsSection}`
          : `#/${view}`;
    if (window.location.hash !== desired) {
      window.history.replaceState(null, "", desired);
    }
  }, [view, selectedProjectId, settingsSection]);

  // Global shortcuts: Cmd/Ctrl+K quick capture, Escape closes the inspector.
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        dispatch({ type: "SET_QUICK_CAPTURE", open: !quickCaptureOpen });
        return;
      }
      // Dialog and Radix layers call preventDefault for Escape they consume,
      // so an unconsumed Escape here means the inspector is the top layer.
      // (Checking the event flag, not state, survives the listener re-binding
      // mid-propagation when a layer's close re-renders this effect.)
      if (e.key === "Escape" && !e.defaultPrevented && selectedActivityId) {
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
          <SyncErrorBanner />
          <main className="relative min-h-0 flex-1 overflow-hidden">
            {children}
            <HintChip />
          </main>
        </div>
        <Inspector />
      </div>
      <QuickCapture />
      <WelcomeDialog />
      <Tour />
    </TooltipProvider>
  );
}
