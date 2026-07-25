import { Button, Dialog } from "@grewelltech/console";

import { CAPTURE_KEY_LABEL } from "@/lib/platform";
import { Kbd } from "@/components/ui/Kbd";
import { useOnboarding } from "./OnboardingProvider";
import { useDialogFocusReassert } from "./useDialogFocusReassert";

const LOOP: { keyword: string; line: string }[] = [
  { keyword: "Capture", line: "Get it out of your head — sorting can wait." },
  { keyword: "Orient", line: "See deadlines, waiting items, and interrupted work." },
  { keyword: "Act", line: "One clear next action per project." },
  { keyword: "Reflect", line: "The timeline shows what actually happened." },
];

/** First-run welcome: what donezo is, the loop, and the one habit.
 *  Escape and backdrop click behave like "Just start". */
export function WelcomeDialog() {
  const { welcomed, tourStep, markWelcomed, startTour } = useOnboarding();
  const open = !welcomed && tourStep === null;
  // Holds focus when reopened via Help > Reset first-run (Radix menus
  // refocus their trigger after closing, stealing it from the dialog).
  const focusRef = useDialogFocusReassert(open);

  return (
    <Dialog
      open={open}
      onClose={markWelcomed}
      title="Welcome to donezo"
      footer={
        <>
          <Button size="sm" variant="ghost" noGlyph onClick={markWelcomed}>
            Just start
          </Button>
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              markWelcomed();
              startTour();
            }}
          >
            Take the tour
          </Button>
        </>
      }
    >
      <div ref={focusRef} className="space-y-4">
        <p className="font-sans text-[0.88rem] leading-relaxed text-gtc-text">
          donezo is a memory for your work. Capture things the moment they happen,
          see where effort actually went, and pick up threads without re-finding
          your place.
        </p>

        <div className="space-y-2">
          {LOOP.map((row) => (
            <div key={row.keyword} className="flex items-baseline gap-3">
              <span className="w-[4.5rem] shrink-0 font-mono text-[0.66rem] uppercase tracking-label text-gtc-accent">
                {row.keyword}
              </span>
              <span className="font-sans text-[0.85rem] text-gtc-text">{row.line}</span>
            </div>
          ))}
        </div>

        <p className="font-sans text-[0.8rem] text-gtc-muted">
          <Kbd>{CAPTURE_KEY_LABEL}</Kbd> captures from anywhere — that&rsquo;s the
          habit that makes the rest work.
        </p>
      </div>
    </Dialog>
  );
}
