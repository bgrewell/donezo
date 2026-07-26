import {
  CalendarRange,
  FolderKanban,
  Inbox,
  ListChecks,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Target,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@grewelltech/console";

import type { ViewId } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { Tip } from "@/components/ui/Tooltip";

const NAV_ITEMS: { id: ViewId; label: string; Icon: LucideIcon }[] = [
  { id: "focus", label: "Focus", Icon: Target },
  { id: "timeline", label: "Timeline", Icon: CalendarRange },
  { id: "inbox", label: "Inbox", Icon: Inbox },
  { id: "projects", label: "Projects", Icon: FolderKanban },
  { id: "review", label: "Review", Icon: ListChecks },
  { id: "search", label: "Search", Icon: Search },
];

/** Narrow collapsible primary navigation rail. */
export function NavRail() {
  const { view, navCollapsed, inbox } = useAppState();
  const dispatch = useAppDispatch();
  const pendingInbox = inbox.filter((i) => i.status === "pending").length;

  return (
    <aside
      className={cn(
        "flex shrink-0 flex-col border-r border-gtc-line bg-gtc-panel transition-[width] duration-150",
        navCollapsed ? "w-[var(--dz-nav-w)]" : "w-[var(--dz-nav-w-expanded)]"
      )}
    >
      {/* Brand mark */}
      <div
        className={cn(
          "flex h-[var(--dz-topbar-h)] shrink-0 items-center border-b border-gtc-line",
          navCollapsed ? "justify-center" : "px-3.5"
        )}
      >
        <span className="select-none font-mono text-[0.8rem] font-semibold uppercase tracking-chrome text-gtc-text">
          {navCollapsed ? (
            <span className="text-gtc-accent">dz</span>
          ) : (
            <>
              donezo <span className="text-gtc-accent">//</span>
            </>
          )}
        </span>
      </div>

      <nav className="flex flex-1 flex-col gap-0.5 py-2" aria-label="Primary">
        {NAV_ITEMS.map(({ id, label, Icon }) => {
          const active = view === id;
          const button = (
            <button
              key={id}
              type="button"
              onClick={() => dispatch({ type: "SET_VIEW", view: id })}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative flex h-9 items-center gap-2.5 font-mono text-[0.72rem] uppercase tracking-chrome outline-none transition-colors",
                navCollapsed ? "justify-center px-0" : "px-3.5",
                active
                  ? "bg-gtc-tint-accent text-gtc-accent"
                  : "text-gtc-muted hover:bg-gtc-tint-accent hover:text-gtc-text",
                "focus-visible:shadow-gtc-focus"
              )}
            >
              {/* Active tick bar */}
              <span
                aria-hidden
                className={cn(
                  "absolute inset-y-1.5 left-0 w-0.5",
                  active ? "bg-gtc-accent shadow-gtc-glow-dot" : "bg-transparent"
                )}
              />
              <span className="relative shrink-0">
                <Icon className="h-4 w-4" aria-hidden />
                {id === "inbox" && pendingInbox > 0 && (
                  <span
                    aria-hidden
                    className="absolute -right-1 -top-1 h-1.5 w-1.5 bg-gtc-warn shadow-gtc-glow-dot"
                  />
                )}
              </span>
              {!navCollapsed && (
                <span className="flex-1 text-left">{label}</span>
              )}
              {!navCollapsed && id === "inbox" && pendingInbox > 0 && (
                <span className="font-mono text-[0.66rem] text-gtc-warn">{pendingInbox}</span>
              )}
            </button>
          );
          return navCollapsed ? (
            <Tip key={id} content={label} side="right">
              {button}
            </Tip>
          ) : (
            button
          );
        })}
      </nav>

      <button
        type="button"
        onClick={() => dispatch({ type: "TOGGLE_NAV" })}
        className={cn(
          "flex h-9 shrink-0 items-center gap-2.5 border-t border-gtc-line font-mono text-[0.68rem] uppercase tracking-chrome text-gtc-muted outline-none transition-colors",
          navCollapsed ? "justify-center" : "px-3.5",
          "hover:text-gtc-text focus-visible:shadow-gtc-focus"
        )}
        aria-label={navCollapsed ? "Expand navigation" : "Collapse navigation"}
      >
        {navCollapsed ? (
          <PanelLeftOpen className="h-4 w-4" aria-hidden />
        ) : (
          <>
            <PanelLeftClose className="h-4 w-4" aria-hidden />
            <span>Collapse</span>
          </>
        )}
      </button>
    </aside>
  );
}
