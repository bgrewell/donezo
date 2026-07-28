import * as React from "react";
import { cn } from "@grewelltech/console";

/** Small mono chip button shared by the capture rows (kind, when, space). */
export const Chip = React.forwardRef<
  HTMLButtonElement,
  React.ButtonHTMLAttributes<HTMLButtonElement> & { selected?: boolean }
>(function Chip({ selected = false, className, children, ...rest }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      aria-pressed={selected}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-gtc border px-2 py-1",
        "font-mono text-[0.64rem] uppercase tracking-chrome outline-none transition-colors",
        "focus-visible:shadow-gtc-focus",
        selected
          ? "border-gtc-accent bg-gtc-tint-accent text-gtc-accent"
          : "border-gtc-line text-gtc-muted hover:text-gtc-text",
        className
      )}
      {...rest}
    >
      {children}
    </button>
  );
});

/** Tiny secondary tag inside a chip ("suggested", the ALT+n shortcut).
 *  max() floors the smallest chrome label at 8px when the small text-size
 *  axis drops the root to 14px. */
export function ChipTag({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <span
      className={cn(
        "font-mono text-[max(0.56rem,8px)] tracking-normal text-gtc-muted",
        className
      )}
    >
      {children}
    </span>
  );
}

/** Quiet inline mono label for compact tailored fields (DUE, WHEN, HOURS…). */
export function QuietLabel({
  htmlFor,
  children,
}: {
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <label
      htmlFor={htmlFor}
      className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted"
    >
      {children}
    </label>
  );
}
