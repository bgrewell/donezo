import * as React from "react";

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
  const [tourStep, setTourStep] = React.useState<number | null>(null);
  const [shortcutsOpen, setShortcutsOpen] = React.useState(false);

  React.useEffect(() => {
    try {
      window.localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(persisted));
    } catch {
      // persistence is best-effort
    }
  }, [persisted]);

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
  }, []);

  const openShortcuts = React.useCallback(() => setShortcutsOpen(true), []);
  const closeShortcuts = React.useCallback(() => setShortcutsOpen(false), []);

  const value = React.useMemo<OnboardingContextValue>(
    () => ({
      ...persisted,
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
