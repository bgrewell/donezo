import * as React from "react";
import { Button, Input, cn } from "@grewelltech/console";

import type { CompletedSource } from "@/domain/types";
import { formatDay, instantOfLocalDay, localDayOfInstant, todayISO } from "@/lib/time";

const META = "font-mono text-[0.64rem] uppercase tracking-label";

/** When something was finished, and the way to correct it.
 *
 *  Completion dates feed work summaries, and some of them are guesses: items
 *  finished before donezo recorded the moment were dated from the activity
 *  logged alongside, and a few could not be dated at all. So the date shows
 *  how sure it is — a guess carries a quiet "~", an unknown says so — and a
 *  click opens a day picker to put it right. "Unknown" is a real answer here,
 *  not a reset: clearing a wrong date leaves the item undated rather than
 *  stamping today.
 *
 *  A day is all a person can reasonably correct to, so that is what is asked
 *  for; it is stored as local noon, which stays on that day wherever it is
 *  read back. */
export function CompletedDateEdit({
  completedAt,
  source,
  onChange,
  prefix = "done",
  className,
}: {
  completedAt?: string;
  source?: CompletedSource;
  /** An instant to set, or undefined for "unknown". */
  onChange: (completedAt: string | undefined) => void;
  /** Leading word of the readout; empty where the context already says it,
   *  as beside a project's status. */
  prefix?: string;
  className?: string;
}) {
  const day = completedAt ? localDayOfInstant(completedAt) : "";
  const [editing, setEditing] = React.useState(false);
  const [value, setValue] = React.useState(day);
  const today = todayISO();

  if (editing) {
    const commit = (next: string | undefined) => {
      onChange(next);
      setEditing(false);
    };
    return (
      <span
        className={cn("flex flex-wrap items-center gap-1.5", className)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            setEditing(false);
          }
        }}
      >
        <Input
          type="date"
          autoFocus
          value={value}
          max={today}
          onChange={(e) => setValue(e.target.value)}
          aria-label="Completed on"
          className="!w-[9.5rem] !py-1 !text-[0.75rem]"
        />
        <Button
          size="sm"
          variant="primary"
          disabled={!value || value > today}
          onClick={() => (value === day ? setEditing(false) : commit(instantOfLocalDay(value)))}
        >
          Save
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={() => commit(undefined)}>
          Unknown
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={() => setEditing(false)}>
          Cancel
        </Button>
      </span>
    );
  }

  const guessed = source === "inferred";
  return (
    <button
      type="button"
      onClick={() => {
        setValue(day);
        setEditing(true);
      }}
      title={
        !completedAt
          ? "When this was finished was never recorded. Click to set it."
          : guessed
            ? "Estimated from the activity logged when it was finished. Click to correct."
            : "Click to correct."
      }
      className={cn(
        META,
        "shrink-0 rounded-gtc px-1 py-0.5 text-gtc-muted outline-none transition-colors",
        "hover:text-gtc-text focus-visible:shadow-gtc-focus",
        className
      )}
    >
      {[prefix, completedAt ? `${guessed ? "~" : ""}${formatDay(day)}` : prefix ? "· date unknown" : "date unknown"]
        .filter(Boolean)
        .join(" ")}
    </button>
  );
}
