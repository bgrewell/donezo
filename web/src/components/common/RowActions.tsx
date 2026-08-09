import * as React from "react";
import { Button, cn } from "@grewelltech/console";

/** One action a row offers. */
export interface RowAction {
  label: string;
  onSelect: () => void;
  /** Marks an action whose effect is not obvious to undo. It gets the danger
   *  styling and, when `confirm` is set, a second click. */
  danger?: boolean;
  /** Short prompt shown before a danger action runs. Without it the action
   *  fires on the first click. */
  confirm?: string;
}

/** Actions revealed on a list row, quiet until reached for.
 *
 *  #29 asks for items to be actionable where they are seen, and asks Focus in
 *  particular to stay calm — the screen you read, not a control panel. So the
 *  controls live in the DOM for keyboard reach but only become visible on
 *  hover or focus, which is the same behaviour NoteRow and the task editor
 *  already had. Sharing it is the point: three copies of this drifted apart
 *  once already.
 *
 *  A danger action with a `confirm` takes two clicks, and the prompt replaces
 *  the buttons rather than appearing beside them — so the second click lands
 *  somewhere the first one was not, and a double-click cannot destroy
 *  anything. */
export function RowActions({
  actions,
  className,
  label = "Row actions",
}: {
  actions: RowAction[];
  className?: string;
  /** Accessible name for the group, when a row needs a more specific one. */
  label?: string;
}) {
  const [confirming, setConfirming] = React.useState<string | null>(null);
  const live = actions.filter(Boolean);
  if (live.length === 0) return null;

  const pending = live.find((a) => a.label === confirming);
  if (pending?.confirm) {
    return (
      <span className={cn("flex shrink-0 items-center gap-2", className)} role="group" aria-label={label}>
        <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          {pending.confirm}
        </span>
        <Button
          size="sm"
          variant="danger"
          noGlyph
          onClick={() => {
            setConfirming(null);
            pending.onSelect();
          }}
        >
          {pending.label}
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={() => setConfirming(null)}>
          Keep
        </Button>
      </span>
    );
  }

  return (
    <span
      role="group"
      aria-label={label}
      className={cn(
        // In the DOM always, so Tab reaches them; visible on hover or when
        // anything inside has focus, so a mouse-free path exists too.
        "flex shrink-0 items-center gap-1 opacity-0 transition-opacity",
        "focus-within:opacity-100 group-hover:opacity-100",
        className
      )}
    >
      {live.map((a) => (
        <Button
          key={a.label}
          size="sm"
          variant={a.danger ? "danger" : "ghost"}
          noGlyph
          onClick={() => (a.confirm ? setConfirming(a.label) : a.onSelect())}
        >
          {a.label}
        </Button>
      ))}
    </span>
  );
}
