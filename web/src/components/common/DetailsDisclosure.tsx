import * as React from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@grewelltech/console";

/** The long form of an item, revealed on demand.
 *
 *  A list is worth having because it can be scanned, so details never take
 *  room until they are asked for — the row stays one line and grows a quiet
 *  marker when there is more to read. Renders nothing at all when there is
 *  not, so a row with no details is exactly the row it was before.
 *
 *  Whitespace is preserved: details are where the multi-line half of a capture
 *  lands, and collapsing that into a paragraph would lose the shape the person
 *  typed. */
export function DetailsDisclosure({
  details,
  label = "details",
  className,
}: {
  details: string;
  /** Named for the entity when it helps — a note's long form is its body. */
  label?: string;
  className?: string;
}) {
  const [open, setOpen] = React.useState(false);
  const id = React.useId();
  if (details.trim() === "") return null;

  return (
    <div className={cn("mt-0.5", className)}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          "inline-flex items-center gap-1 rounded-gtc py-0.5 pr-1",
          "font-mono text-[0.62rem] uppercase tracking-label outline-none transition-colors",
          "focus-visible:shadow-gtc-focus",
          open ? "text-gtc-accent" : "text-gtc-muted hover:text-gtc-text"
        )}
      >
        <ChevronRight
          size={11}
          className={cn("transition-transform", open && "rotate-90")}
          aria-hidden
        />
        {open ? `hide ${label}` : label}
      </button>
      {open && (
        <p
          id={id}
          className="whitespace-pre-wrap pb-1 pl-3.5 pr-2 font-sans text-[0.8rem] leading-relaxed text-gtc-muted"
        >
          {details}
        </p>
      )}
    </div>
  );
}
