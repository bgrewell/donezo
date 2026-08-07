import * as React from "react";

import { fetchUserSettings, saveUserSettings, type UserSettings } from "@/api/client";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { TOUR_STEPS } from "./steps";

/** localStorage key for persisted first-run state. */
export const ONBOARDING_STORAGE_KEY = "donezo.onboarding.v1";

/** Slice of onboarding state that survives reloads. */
export interface OnboardingPersisted {
  /** Welcome dialog has been acknowledged (either footer action). */
  welcomed: boolean;
  /** Tour was completed or skipped at least once. */
  tourDone: boolean;
  /** Ids of hint chips the user has dismissed. */
  dismissedHints: string[];
}

export interface OnboardingContextValue extends OnboardingPersisted {
  /** False until the server's copy of this state has been read (or the read
   *  has failed). First-run UI must wait for it: localStorage is per-browser,
   *  so on a new browser the local answer is "never seen it" and showing the
   *  welcome on that basis flashes it up before the server corrects it. */
  hydrated: boolean;
  /** Active tour step index, or null when no tour is running. */
  tourStep: number | null;
  /** Whether the keyboard-shortcuts sheet is open. */
  shortcutsOpen: boolean;
  markWelcomed: () => void;
  /** Switch to the timeline and begin the tour at step 0. */
  startTour: () => void;
  /** Stop the tour; both completing and skipping mark it done. */
  endTour: (completed: boolean) => void;
  nextStep: () => void;
  prevStep: () => void;
  dismissHint: (id: string) => void;
  /** Clear persisted first-run state so the welcome shows again. */
  resetFirstRun: () => void;
  openShortcuts: () => void;
  closeShortcuts: () => void;
}

const DEFAULTS: OnboardingPersisted = {
  welcomed: false,
  tourDone: false,
  dismissedHints: [],
};

const OnboardingContext = React.createContext<OnboardingContextValue | null>(null);

function loadPersisted(): OnboardingPersisted {
  try {
    const raw = window.localStorage.getItem(ONBOARDING_STORAGE_KEY);
    if (!raw) return DEFAULTS;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return DEFAULTS;
    const p = parsed as Partial<OnboardingPersisted>;
    return {
      welcomed: p.welcomed === true,
      tourDone: p.tourDone === true,
      dismissedHints: Array.isArray(p.dismissedHints)
        ? p.dismissedHints.filter((h): h is string => typeof h === "string")
        : [],
    };
  } catch {
    // localStorage unavailable or corrupt — treat as a fresh profile
    return DEFAULTS;
  }
}

/** Union of local onboarding state and whatever the server holds.
 *
 *  Progress only ever accumulates: a flag set on either side stays set, and
 *  dismissed hints combine. Neither side is authoritative — the server may
 *  know about another browser, and this browser may have just recorded
 *  something not yet written — so the merge has no loser. */
function mergeOnboarding(
  local: OnboardingPersisted,
  remote: Pick<UserSettings, "welcomed" | "tourDone" | "dismissedHints">
): OnboardingPersisted {
  const remoteHints = Array.isArray(remote.dismissedHints) ? remote.dismissedHints : [];
  return {
    welcomed: local.welcomed || remote.welcomed === true,
    tourDone: local.tourDone || remote.tourDone === true,
    dismissedHints: Array.from(new Set([...local.dismissedHints, ...remoteHints])),
  };
}

/** True when the keypress originated in a text-entry control. */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return (
    tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable
  );
}

/** First-run onboarding state: welcome, tour progress, hints, shortcuts sheet.
 *  Persisted (best-effort) under "donezo.onboarding.v1"; tour position is
 *  runtime-only. Also owns the global "?" shortcut for the shortcuts sheet. */
export function OnboardingProvider({ children }: { children: React.ReactNode }) {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const [persisted, setPersisted] = React.useState<OnboardingPersisted>(loadPersisted);
  const [hydrated, setHydrated] = React.useState(false);
  const [tourStep, setTourStep] = React.useState<number | null>(null);
  const [shortcutsOpen, setShortcutsOpen] = React.useState(false);

  React.useEffect(() => {
    try {
      window.localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(persisted));
    } catch {
      // persistence is best-effort
    }
  }, [persisted]);

  // Onboarding progress is account state, not browser state: having seen the
  // welcome once should mean it never returns, on any machine. localStorage
  // above stays as a cache so an offline or failed load still behaves, but
  // the server is what makes the answer follow the person.
  //
  // Reads merge rather than overwrite, for the same reason the server merges
  // one way: local state may legitimately be ahead of the server (dismissed
  // something a moment ago, write still in flight), and the server may be
  // ahead of local (a different browser). Taking the union of both loses
  // neither.
  const remote = React.useRef<OnboardingPersisted | null>(null);
  React.useEffect(() => {
    let cancelled = false;
    fetchUserSettings()
      .then((settings) => {
        if (cancelled) return;
        setPersisted((local) => {
          const merged = mergeOnboarding(local, settings);
          remote.current = merged;
          return merged;
        });
      })
      .catch(() => {
        if (cancelled) return;
        // Could not read — offline, or the session lapsed. Treat local as the
        // baseline so a later deliberate change still gets a chance to save,
        // and let the first-run UI proceed rather than hang on a dialog that
        // never appears.
        remote.current = null;
      })
      .finally(() => {
        if (!cancelled) setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Write through once hydrated. Sending before the first read would push this
  // browser's empty state at a server that may know better; the server refuses
  // to regress the flags anyway, but not sending is the cheaper guarantee.
  React.useEffect(() => {
    const prev = remote.current;
    if (!hydrated || prev === null) return;
    const patch: UserSettings = {};
    if (persisted.welcomed && !prev.welcomed) patch.welcomed = true;
    if (persisted.tourDone && !prev.tourDone) patch.tourDone = true;
    const newHints = persisted.dismissedHints.filter((h) => !prev.dismissedHints.includes(h));
    if (newHints.length > 0) patch.dismissedHints = newHints;
    if (Object.keys(patch).length === 0) return;

    remote.current = mergeOnboarding(prev, patch);
    void saveUserSettings(patch).catch(() => {
      // Best-effort: the change is applied locally and cached in
      // localStorage, so the only cost is that it does not follow the user
      // to another machine until something writes successfully.
    });
  }, [persisted, hydrated]);

  // Quick capture (Ctrl/⌘+K) opens over any screen, so the welcome and the
  // shortcuts sheet yield to it instead of stacking — two GTC Dialogs paint
  // in mount order and resolve Escape oldest-first, which would leave capture
  // typing into a hidden input and Escape closing the bottom layer. Using the
  // advertised capture habit counts as acknowledging the welcome (same as
  // "Just start").
  const quickCaptureOpen = state.quickCaptureOpen;
  React.useEffect(() => {
    if (!quickCaptureOpen) return;
    setShortcutsOpen(false);
    setPersisted((p) => (p.welcomed ? p : { ...p, welcomed: true }));
  }, [quickCaptureOpen]);

  // Global "?" opens the shortcuts sheet — but never over a dialog, never
  // while a tour runs (its card leaves the DOM during step transitions, so
  // check state rather than [role="dialog"] presence), and never while typing.
  const tourStepRef = React.useRef(tourStep);
  tourStepRef.current = tourStep;
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "?" || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.defaultPrevented || isTypingTarget(e.target)) return;
      if (tourStepRef.current !== null) return;
      if (document.querySelector('[role="dialog"]')) return;
      e.preventDefault();
      setShortcutsOpen(true);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const markWelcomed = React.useCallback(() => {
    setPersisted((p) => (p.welcomed ? p : { ...p, welcomed: true }));
  }, []);

  const view = state.view;
  const startTour = React.useCallback(() => {
    setShortcutsOpen(false);
    // Only navigate when needed — SET_VIEW clears the activity selection,
    // and a tour started from the timeline must not close the inspector.
    if (view !== "timeline") dispatch({ type: "SET_VIEW", view: "timeline" });
    setTourStep(0);
  }, [dispatch, view]);

  const endTour = React.useCallback((_completed: boolean) => {
    setTourStep(null);
    setPersisted((p) => (p.tourDone ? p : { ...p, tourDone: true }));
  }, []);

  const nextStep = React.useCallback(() => {
    if (tourStep === null) return;
    if (tourStep >= TOUR_STEPS.length - 1) endTour(true);
    else setTourStep(tourStep + 1);
  }, [tourStep, endTour]);

  const prevStep = React.useCallback(() => {
    if (tourStep === null || tourStep === 0) return;
    setTourStep(tourStep - 1);
  }, [tourStep]);

  const dismissHint = React.useCallback((id: string) => {
    setPersisted((p) =>
      p.dismissedHints.includes(id)
        ? p
        : { ...p, dismissedHints: [...p.dismissedHints, id] }
    );
  }, []);

  const resetFirstRun = React.useCallback(() => {
    try {
      window.localStorage.removeItem(ONBOARDING_STORAGE_KEY);
    } catch {
      // best-effort
    }
    setPersisted(DEFAULTS);
    setTourStep(null);
    setShortcutsOpen(false);
    // Clearing locally is not enough now that the server holds this too — it
    // would be merged straight back on the next read. This is the one action
    // that legitimately moves progress backwards, so it goes over the wire as
    // an explicit intent rather than as flags set to false, which the server
    // ignores by design.
    remote.current = DEFAULTS;
    void saveUserSettings({ resetOnboarding: true }).catch(() => {
      // Best-effort, but a failure here means the reset is local-only and the
      // next successful read will restore the old state. Nothing is lost, and
      // the action can simply be repeated.
    });
  }, []);

  const openShortcuts = React.useCallback(() => setShortcutsOpen(true), []);
  const closeShortcuts = React.useCallback(() => setShortcutsOpen(false), []);

  const value = React.useMemo<OnboardingContextValue>(
    () => ({
      ...persisted,
      hydrated,
      tourStep,
      shortcutsOpen,
      markWelcomed,
      startTour,
      endTour,
      nextStep,
      prevStep,
      dismissHint,
      resetFirstRun,
      openShortcuts,
      closeShortcuts,
    }),
    [
      persisted,
      hydrated,
      tourStep,
      shortcutsOpen,
      markWelcomed,
      startTour,
      endTour,
      nextStep,
      prevStep,
      dismissHint,
      resetFirstRun,
      openShortcuts,
      closeShortcuts,
    ]
  );

  return <OnboardingContext.Provider value={value}>{children}</OnboardingContext.Provider>;
}

export function useOnboarding(): OnboardingContextValue {
  const ctx = React.useContext(OnboardingContext);
  if (!ctx) throw new Error("useOnboarding must be used within OnboardingProvider");
  return ctx;
}
