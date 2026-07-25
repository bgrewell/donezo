import { X } from "lucide-react";
import { cn } from "@grewelltech/console";

import { useAppState } from "@/state/AppStore";
import { useOnboarding } from "./OnboardingProvider";

/** Hint id persisted in dismissedHints. */
export const TIMELINE_HINT_ID = "timeline-log-cell";

/** Quiet timeline-only hint chip, bottom-right of the timeline pane.
 *  Shows once the welcome is done, while no tour runs, until dismissed. */
export function HintChip() {
  const { view } = useAppState();
  const { welcomed, tourStep, dismissedHints, dismissHint } = useOnboarding();

  const show =
    view === "timeline" &&
    welcomed &&
    tourStep === null &&
    !dismissedHints.includes(TIMELINE_HINT_ID);
  if (!show) return null;

  return (
    <div
      className={cn(
        "absolute bottom-3 right-3 z-20 flex items-center gap-2",
        "rounded-gtc border border-gtc-line bg-gtc-inset px-2.5 py-1.5"
      )}
    >
      <span className="font-sans text-[0.75rem] text-gtc-text">
        Click any empty cell to log what happened.
      </span>
      <button
        type="button"
        aria-label="Dismiss hint"
        onClick={() => dismissHint(TIMELINE_HINT_ID)}
        className={cn(
          "flex h-4 w-4 shrink-0 items-center justify-center rounded-gtc text-gtc-muted",
          "outline-none transition-colors hover:text-gtc-text focus-visible:shadow-gtc-focus"
        )}
      >
        <X className="h-3 w-3" aria-hidden />
      </button>
    </div>
  );
}
