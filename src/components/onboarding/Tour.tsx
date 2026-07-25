import * as React from "react";
import { Button } from "@grewelltech/console";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { useOnboarding } from "./OnboardingProvider";
import { TOUR_STEPS, type TourPlacement } from "./steps";

/** Screen-fraction dim around the highlighted target. */
const DIM = "rgba(5,11,19,0.62)";
/** Breathing room between the target edge and the cutout edge. */
const CUTOUT_PAD = 4;
/** Minimum distance from the viewport edge for the coachmark card. */
const VIEWPORT_PAD = 12;
/** Gap between the target and the coachmark card. */
const CARD_GAP = 10;
/** Card width — must match the w-72 class on the card. */
const CARD_W = 288;
/** Frames to wait for the target after a view switch before giving up. */
const LOCATE_TRIES = 40;

interface Box {
  top: number;
  left: number;
  width: number;
  height: number;
}

function toBox(r: DOMRect): Box {
  return { top: r.top, left: r.left, width: r.width, height: r.height };
}

function boxesEqual(a: Box, b: Box): boolean {
  return (
    Math.abs(a.top - b.top) < 0.5 &&
    Math.abs(a.left - b.left) < 0.5 &&
    Math.abs(a.width - b.width) < 0.5 &&
    Math.abs(a.height - b.height) < 0.5
  );
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(Math.max(v, lo), Math.max(lo, hi));
}

/** Cutout box: target padded slightly, clamped to the viewport. */
function cutoutFor(box: Box): Box {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const top = Math.max(0, box.top - CUTOUT_PAD);
  const left = Math.max(0, box.left - CUTOUT_PAD);
  const bottom = Math.min(vh, box.top + box.height + CUTOUT_PAD);
  const right = Math.min(vw, box.left + box.width + CUTOUT_PAD);
  return { top, left, width: Math.max(0, right - left), height: Math.max(0, bottom - top) };
}

/** Card position with basic flip-to-fit: preferred side first, then the
 *  rest; if nothing fits, clamp the preferred position into the viewport. */
function placeCard(
  cutout: Box | null,
  placement: TourPlacement,
  cardW: number,
  cardH: number
): { top: number; left: number } {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  if (!cutout) {
    return {
      top: Math.max(VIEWPORT_PAD, (vh - cardH) / 2),
      left: Math.max(VIEWPORT_PAD, (vw - cardW) / 2),
    };
  }
  const right = cutout.left + cutout.width;
  const bottom = cutout.top + cutout.height;
  const centerX = clamp(
    cutout.left + cutout.width / 2 - cardW / 2,
    VIEWPORT_PAD,
    vw - VIEWPORT_PAD - cardW
  );
  const centerY = clamp(
    cutout.top + cutout.height / 2 - cardH / 2,
    VIEWPORT_PAD,
    vh - VIEWPORT_PAD - cardH
  );
  const candidates: Record<TourPlacement, { top: number; left: number }> = {
    below: { top: bottom + CARD_GAP, left: centerX },
    above: { top: cutout.top - CARD_GAP - cardH, left: centerX },
    right: { top: centerY, left: right + CARD_GAP },
    left: { top: centerY, left: cutout.left - CARD_GAP - cardW },
  };
  const order: TourPlacement[] = [
    placement,
    ...(["below", "above", "right", "left"] as TourPlacement[]).filter(
      (p) => p !== placement
    ),
  ];
  for (const p of order) {
    const c = candidates[p];
    if (
      c.top >= VIEWPORT_PAD &&
      c.left >= VIEWPORT_PAD &&
      c.top + cardH <= vh - VIEWPORT_PAD &&
      c.left + cardW <= vw - VIEWPORT_PAD
    ) {
      return c;
    }
  }
  // Nothing fits beside the target (it may fill the pane) — keep the card
  // on-viewport at the preferred side, overlapping the target if needed.
  const preferred = candidates[placement];
  return {
    top: clamp(preferred.top, VIEWPORT_PAD, vh - VIEWPORT_PAD - cardH),
    left: clamp(preferred.left, VIEWPORT_PAD, vw - VIEWPORT_PAD - cardW),
  };
}

/** Guided tour overlay: dims everything but the current step's target and
 *  anchors a coachmark card beside it. Rendered only while a tour runs. */
export function Tour() {
  const { tourStep } = useOnboarding();
  if (tourStep === null) return null;
  return <TourOverlay stepIndex={tourStep} />;
}

function TourOverlay({ stepIndex }: { stepIndex: number }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const { nextStep, prevStep, endTour } = useOnboarding();

  const step = TOUR_STEPS[stepIndex];
  const last = stepIndex === TOUR_STEPS.length - 1;

  /** Target box in viewport coords; null while locating or when missing. */
  const [box, setBox] = React.useState<Box | null>(null);
  /** True once locating gave up — card centers over a full dim. */
  const [missing, setMissing] = React.useState(false);
  const [cardPos, setCardPos] = React.useState<{ top: number; left: number } | null>(null);

  const targetRef = React.useRef<HTMLElement | null>(null);
  const cardRef = React.useRef<HTMLDivElement>(null);

  // Latest handlers for the stable document-level key listener.
  const handlersRef = React.useRef({ nextStep, prevStep, endTour });
  handlersRef.current = { nextStep, prevStep, endTour };
  const viewRef = React.useRef(state.view);
  viewRef.current = state.view;

  // Restore focus to whatever had it before the tour when the tour ends.
  // The restore target is (re)captured whenever the card takes focus from an
  // outside element — the opening menu item unmounts immediately, but its
  // menu refocuses the trigger just after, and that is what we want back.
  const restoreRef = React.useRef<HTMLElement | null>(
    typeof document !== "undefined" && document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null
  );
  React.useEffect(() => {
    return () => {
      const el = restoreRef.current;
      if (el && el.isConnected) el.focus();
    };
  }, []);

  // Per step: switch views when needed, then find the target once the view
  // has painted (requestAnimationFrame retries).
  React.useEffect(() => {
    if (step.view !== "any" && viewRef.current !== step.view) {
      dispatch({ type: "SET_VIEW", view: step.view });
    }
    setBox(null);
    setMissing(false);
    setCardPos(null);
    targetRef.current = null;
    let raf = 0;
    let tries = 0;
    const locate = () => {
      const el = document.querySelector<HTMLElement>(`[data-tour="${step.target}"]`);
      if (el) {
        targetRef.current = el;
        const r = el.getBoundingClientRect();
        if (
          r.top < 0 ||
          r.left < 0 ||
          r.bottom > window.innerHeight ||
          r.right > window.innerWidth
        ) {
          el.scrollIntoView({ block: "nearest" });
        }
        setBox(toBox(el.getBoundingClientRect()));
        return;
      }
      if (tries++ < LOCATE_TRIES) raf = requestAnimationFrame(locate);
      else setMissing(true);
    };
    raf = requestAnimationFrame(locate);
    return () => cancelAnimationFrame(raf);
  }, [stepIndex, step, dispatch]);

  // Track the target while active: resize, any scroll (capture phase), and
  // a slow interval as a fallback for silent layout shifts.
  React.useEffect(() => {
    const update = () => {
      const el = targetRef.current;
      if (!el || !el.isConnected) return;
      const next = toBox(el.getBoundingClientRect());
      setBox((prev) => (prev && boxesEqual(prev, next) ? prev : next));
    };
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    const interval = window.setInterval(update, 250);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
      window.clearInterval(interval);
    };
  }, []);

  const cutout = box ? cutoutFor(box) : null;
  const settled = box !== null || missing;

  // Position the card after it has rendered (its height depends on copy).
  React.useLayoutEffect(() => {
    if (!settled) return;
    const card = cardRef.current;
    if (!card) return;
    setCardPos(
      placeCard(cutout, step.placement, card.offsetWidth || CARD_W, card.offsetHeight)
    );
    // cutout is derived from box, which is a dependency.
  }, [settled, box, missing, stepIndex]); // eslint-disable-line react-hooks/exhaustive-deps

  // Focus the card on each step once it is positioned (a hidden card cannot
  // take focus). The opener's menu restores focus to its trigger just after
  // closing, so re-assert once shortly after mounting.
  const positioned = cardPos !== null;
  React.useEffect(() => {
    if (!settled || !positioned) return;
    const takeFocus = () => {
      const card = cardRef.current;
      if (!card || card.contains(document.activeElement)) return;
      const active = document.activeElement;
      if (active instanceof HTMLElement && active !== document.body) {
        restoreRef.current = active;
      }
      card.focus();
    };
    takeFocus();
    const t = window.setTimeout(takeFocus, 120);
    return () => window.clearTimeout(t);
  }, [settled, positioned, stepIndex]);

  // Keyboard: Escape skips, arrows navigate, Enter advances, Tab is trapped
  // in the card. Escape follows the layering convention (preventDefault so
  // the AppShell handler leaves the inspector alone). If some other dialog
  // is stacked above the tour, defer everything to it.
  React.useEffect(() => {
    // A Radix menu item activates on keydown, and React mounts this overlay
    // synchronously — so this listener attaches before that same Enter
    // reaches document (bubble phase) and would advance past step 1. Ignore
    // any event created before the listener existed.
    const mountedAt = performance.now();
    const onKey = (e: KeyboardEvent) => {
      if (e.timeStamp <= mountedAt) return;
      const card = cardRef.current;
      const dialogs = Array.from(document.querySelectorAll('[role="dialog"]'));
      if (dialogs.some((d) => d !== card)) return;

      if (e.key === "Escape") {
        if (e.defaultPrevented) return;
        e.preventDefault();
        handlersRef.current.endTour(false);
        return;
      }
      if (e.key === "ArrowRight") {
        e.preventDefault();
        handlersRef.current.nextStep();
        return;
      }
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        handlersRef.current.prevStep();
        return;
      }
      if (e.key === "Enter") {
        // A focused card button keeps its native activation.
        if (
          e.target instanceof HTMLElement &&
          e.target.tagName === "BUTTON" &&
          card?.contains(e.target)
        ) {
          return;
        }
        e.preventDefault();
        handlersRef.current.nextStep();
        return;
      }
      if (e.key === "Tab") {
        if (!card) return;
        const tabbables = Array.from(
          card.querySelectorAll<HTMLElement>("button:not([disabled])")
        ).filter((el) => el.offsetParent !== null);
        if (tabbables.length === 0) {
          e.preventDefault();
          card.focus();
          return;
        }
        const first = tabbables[0];
        const lastEl = tabbables[tabbables.length - 1];
        const active = document.activeElement;
        if (e.shiftKey) {
          if (active === first || active === card || !card.contains(active)) {
            e.preventDefault();
            lastEl.focus();
          }
        } else if (active === lastEl || !card.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="fixed inset-0 z-50">
      {/* Dim: four rects around the cutout (or one full-screen rect). Outer
          edges anchor to the viewport so resizes never leave gaps. The dim
          blocks pointer events; only the card below is interactive. */}
      {cutout ? (
        <>
          <div
            className="absolute"
            style={{ top: 0, left: 0, right: 0, height: cutout.top, background: DIM }}
          />
          <div
            className="absolute"
            style={{
              top: cutout.top,
              left: 0,
              width: cutout.left,
              height: cutout.height,
              background: DIM,
            }}
          />
          <div
            className="absolute"
            style={{
              top: cutout.top,
              left: cutout.left + cutout.width,
              right: 0,
              height: cutout.height,
              background: DIM,
            }}
          />
          <div
            className="absolute"
            style={{
              top: cutout.top + cutout.height,
              left: 0,
              right: 0,
              bottom: 0,
              background: DIM,
            }}
          />
          <div
            aria-hidden
            className="pointer-events-none absolute rounded-gtc"
            style={{
              top: cutout.top,
              left: cutout.left,
              width: cutout.width,
              height: cutout.height,
              outline: "1px solid var(--gtc-accent-dim)",
              outlineOffset: -1,
            }}
          />
        </>
      ) : (
        <div className="absolute inset-0" style={{ background: DIM }} />
      )}

      {settled && (
        <div
          ref={cardRef}
          role="dialog"
          aria-modal="true"
          aria-label={step.title}
          tabIndex={-1}
          className="absolute w-72 rounded-gtc border border-gtc-line bg-gtc-panel bg-gtc-sheen p-3.5 outline-none"
          style={
            cardPos
              ? { top: cardPos.top, left: cardPos.left }
              : { top: 0, left: 0, visibility: "hidden" }
          }
        >
          <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            {stepIndex + 1} / {TOUR_STEPS.length}
          </div>
          <div className="mt-1 font-sans text-[0.9rem] font-medium text-gtc-text">
            {step.title}
          </div>
          <p className="mt-1 font-sans text-[0.8rem] leading-relaxed text-gtc-muted">
            {step.body}
          </p>
          <div className="mt-3 flex items-center gap-2">
            <button
              type="button"
              onClick={() => endTour(false)}
              className="text-left font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted transition-colors hover:text-gtc-text"
            >
              Skip tour
            </button>
            <div className="flex-1" />
            {stepIndex > 0 && (
              <Button size="sm" variant="ghost" noGlyph onClick={prevStep}>
                Back
              </Button>
            )}
            <Button size="sm" variant="primary" noGlyph onClick={nextStep}>
              {last ? "Done" : "Next"}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
