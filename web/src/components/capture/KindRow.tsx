import type { ItemKind } from "@/domain/types";
import { IS_MAC } from "@/lib/platform";
import { Chip, ChipTag } from "./chips";

/** Capture kinds in display (and Alt+1..5) order. */
export const KINDS: ItemKind[] = ["task", "note", "reminder", "activity", "project"];

/** Alt+n tag rendered on each kind chip: ⌥1 on macOS, ALT+1 elsewhere. */
function altLabel(n: number): string {
  return IS_MAC ? `⌥${n}` : `ALT+${n}`;
}

/** The five kind chips. Selection follows the auto-suggest heuristics until
 *  a manual pick (chip click or Alt+1..5); the suggested tag only shows
 *  while the selection is still the heuristic's. */
export function KindRow({
  kind,
  showSuggested,
  onPick,
}: {
  kind: ItemKind;
  /** True while the selection comes from auto-suggest (no manual pick yet). */
  showSuggested: boolean;
  onPick: (kind: ItemKind) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Item kind">
      {KINDS.map((k, i) => {
        const selected = kind === k;
        return (
          <Chip key={k} selected={selected} onClick={() => onPick(k)}>
            {k}
            {/* The suggested tag replaces the shortcut tag — the shortcut
                is redundant on the already-selected chip, and showing both
                wraps the row. */}
            {selected && showSuggested ? (
              <ChipTag className="lowercase">suggested</ChipTag>
            ) : (
              <ChipTag>{altLabel(i + 1)}</ChipTag>
            )}
          </Chip>
        );
      })}
    </div>
  );
}
