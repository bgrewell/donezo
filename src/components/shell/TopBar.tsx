import { Bell, CircleHelp, Palette, Plus, Search } from "lucide-react";
import { Avatar, Button, cn } from "@grewelltech/console";

import type { ViewId } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { THEMES } from "@/lib/themes";
import { useTheme } from "@/state/ThemeProvider";
import { relativeFromToday } from "@/lib/time";
import { CAPTURE_KEY_LABEL } from "@/lib/platform";
import { Kbd } from "@/components/ui/Kbd";
import { useOnboarding } from "@/components/onboarding/OnboardingProvider";
import { ShortcutsSheet } from "@/components/onboarding/ShortcutsSheet";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/DropdownMenu";

const VIEW_TITLES: Record<ViewId, string> = {
  focus: "Focus",
  timeline: "Timeline",
  inbox: "Inbox",
  projects: "Projects",
  review: "Review",
  search: "Search",
};

/** Compact top bar: view title, global search, capture, reminders, theme, user. */
export function TopBar() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { theme, setTheme } = useTheme();
  const { startTour, openShortcuts, resetFirstRun } = useOnboarding();

  const openReminders = state.reminders
    .filter((r) => !r.done)
    .sort((a, b) => a.remindAt.localeCompare(b.remindAt));

  return (
    <header className="flex h-[var(--dz-topbar-h)] shrink-0 items-center gap-3 border-b border-gtc-line bg-gtc-panel px-3">
      <h1 className="min-w-0 shrink-0 font-mono text-[0.8rem] font-semibold uppercase tracking-chrome text-gtc-text">
        {VIEW_TITLES[state.view]}
      </h1>

      <div className="relative ml-2 hidden md:block">
        <Search
          className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gtc-muted"
          aria-hidden
        />
        <input
          type="search"
          value={state.searchQuery}
          onChange={(e) =>
            dispatch({ type: "SET_SEARCH_QUERY", query: e.target.value })
          }
          onKeyDown={(e) => {
            if (e.key === "Enter") dispatch({ type: "SET_VIEW", view: "search" });
          }}
          placeholder="Search everything…"
          aria-label="Global search"
          className={cn(
            "h-7 w-60 rounded-gtc border border-gtc-line bg-gtc-inset pl-7 pr-2",
            "font-sans text-[0.8rem] text-gtc-text placeholder:text-gtc-muted",
            "outline-none transition-colors focus:border-gtc-accent focus:shadow-gtc-focus"
          )}
        />
      </div>

      <div className="flex-1" />

      <Button
        size="sm"
        variant="primary"
        noGlyph
        onClick={() => dispatch({ type: "SET_QUICK_CAPTURE", open: true })}
        className="gap-1.5"
        data-tour="capture"
      >
        <Plus className="h-3.5 w-3.5" aria-hidden />
        Capture
        <Kbd className="ml-1 hidden lg:inline-flex">{CAPTURE_KEY_LABEL}</Kbd>
      </Button>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label={`Reminders (${openReminders.length})`}
            className={cn(
              "relative flex h-7 w-7 items-center justify-center rounded-gtc border border-gtc-line text-gtc-muted",
              "outline-none transition-colors hover:border-gtc-accent-dim hover:text-gtc-text focus-visible:shadow-gtc-focus"
            )}
          >
            <Bell className="h-3.5 w-3.5" aria-hidden />
            {openReminders.length > 0 && (
              <span
                aria-hidden
                className="absolute -right-0.5 -top-0.5 h-1.5 w-1.5 bg-gtc-accent shadow-gtc-glow-dot"
              />
            )}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-80">
          <DropdownMenuLabel>Reminders</DropdownMenuLabel>
          {openReminders.length === 0 && (
            <div className="px-3 py-2 text-[0.8rem] text-gtc-muted">Nothing scheduled.</div>
          )}
          {openReminders.slice(0, 6).map((r) => (
            <DropdownMenuItem
              key={r.id}
              className="items-start gap-2 normal-case tracking-normal"
              onSelect={() => dispatch({ type: "UPDATE_REMINDER", id: r.id, patch: { done: true } })}
            >
              <span className="mt-1 h-1.5 w-1.5 shrink-0 bg-gtc-accent" aria-hidden />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-sans text-[0.8rem] normal-case text-gtc-text">
                  {r.text}
                </span>
                <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                  {relativeFromToday(r.remindAt.slice(0, 10))} · select to mark done
                </span>
              </span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="Theme"
            className={cn(
              "flex h-7 w-7 items-center justify-center rounded-gtc border border-gtc-line text-gtc-muted",
              "outline-none transition-colors hover:border-gtc-accent-dim hover:text-gtc-text focus-visible:shadow-gtc-focus"
            )}
          >
            <Palette className="h-3.5 w-3.5" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Theme</DropdownMenuLabel>
          {THEMES.map((t) => (
            <DropdownMenuCheckboxItem
              key={t.id}
              checked={theme === t.id}
              onCheckedChange={() => setTheme(t.id)}
            >
              {t.label}
            </DropdownMenuCheckboxItem>
          ))}
          <DropdownMenuSeparator />
          <div className="px-3 pb-1.5 text-[0.7rem] normal-case text-gtc-muted">
            Themes are token overrides — add more in themes.css.
          </div>
        </DropdownMenuContent>
      </DropdownMenu>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="Help"
            className={cn(
              "flex h-7 w-7 items-center justify-center rounded-gtc border border-gtc-line text-gtc-muted",
              "outline-none transition-colors hover:border-gtc-accent-dim hover:text-gtc-text focus-visible:shadow-gtc-focus"
            )}
          >
            <CircleHelp className="h-3.5 w-3.5" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Help</DropdownMenuLabel>
          <DropdownMenuItem onSelect={() => startTour()}>Take the tour</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => openShortcuts()}>
            Keyboard shortcuts
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => resetFirstRun()}>
            Reset first-run
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Avatar name="Ben Grewell" size="sm" />
      <ShortcutsSheet />
    </header>
  );
}
