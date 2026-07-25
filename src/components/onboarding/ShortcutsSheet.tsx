import { Dialog } from "@grewelltech/console";

import { CAPTURE_KEY_LABEL, MOD_LABEL } from "@/lib/platform";
import { Kbd } from "@/components/ui/Kbd";
import { useOnboarding } from "./OnboardingProvider";

const ROWS: { keys: string[]; text: string }[] = [
  { keys: [CAPTURE_KEY_LABEL], text: "Quick capture from anywhere" },
  { keys: ["Enter"], text: "Create the typed item (in capture)" },
  { keys: [`${MOD_LABEL}+Enter`], text: "Save the capture to the inbox" },
  { keys: ["Esc"], text: "Close the top layer" },
  { keys: ["?"], text: "This sheet" },
  { keys: ["←", "→", "Enter", "Esc"], text: "In the tour — back · next · skip" },
];

/** Keyboard shortcuts reference. Opens from the Help menu or "?". */
export function ShortcutsSheet() {
  const { shortcutsOpen, closeShortcuts } = useOnboarding();

  return (
    <Dialog open={shortcutsOpen} onClose={closeShortcuts} title="Keyboard shortcuts">
      <div className="space-y-2.5">
        {ROWS.map((row) => (
          <div key={row.text} className="flex items-baseline gap-4">
            <span className="flex w-36 shrink-0 flex-wrap gap-1">
              {row.keys.map((k) => (
                <Kbd key={k}>{k}</Kbd>
              ))}
            </span>
            <span className="font-sans text-[0.85rem] text-gtc-text">{row.text}</span>
          </div>
        ))}
      </div>
    </Dialog>
  );
}
