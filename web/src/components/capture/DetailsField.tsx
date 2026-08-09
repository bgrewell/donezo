import * as React from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@grewelltech/console";

/** The optional long form, collapsed until reached for.
 *
 *  Capture costing zero decisions is load-bearing, so the one-line fast path
 *  has to stay exactly as fast as it was: this is a single quiet control until
 *  someone opens it, and opening it is the only thing that puts a second field
 *  on screen. It stays open once there is something in it, because a filled
 *  field that hides itself reads as lost text. */
export function DetailsField({
  value,
  onChange,
  label = "Details",
  placeholder,
  rows = 4,
}: {
  value: string;
  onChange: (value: string) => void;
  /** Overridden for a note, where the long form is the body. */
  label?: string;
  placeholder?: string;
  rows?: number;
}) {
  const [open, setOpen] = React.useState(false);
  const id = React.useId();
  const shown = open || value !== "";

  return (
    <div className="space-y-1.5">
      <button
        type="button"
        aria-expanded={shown}
        aria-controls={shown ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          "inline-flex items-center gap-1 rounded-gtc px-1 py-0.5 -ml-1",
          "font-mono text-[0.62rem] uppercase tracking-label outline-none transition-colors",
          "focus-visible:shadow-gtc-focus",
          shown ? "text-gtc-accent" : "text-gtc-muted hover:text-gtc-text"
        )}
      >
        <ChevronRight
          size={11}
          className={cn("transition-transform", shown && "rotate-90")}
          aria-hidden
        />
        {label}
        {!shown && <span className="normal-case tracking-normal"> — optional</span>}
      </button>
      {shown && (
        <textarea
          id={id}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={rows}
          placeholder={placeholder}
          aria-label={label}
          className={cn(
            "w-full rounded-gtc border border-gtc-line bg-gtc-inset px-2 py-1.5",
            "font-sans text-[0.8rem] text-gtc-text placeholder:text-gtc-muted",
            "focus:border-gtc-accent focus:outline-none"
          )}
        />
      )}
    </div>
  );
}
