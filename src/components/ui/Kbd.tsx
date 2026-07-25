import type { ReactNode } from "react";
import { cn } from "@grewelltech/console";

/** Compact keyboard-shortcut chip, e.g. <Kbd>⌘K</Kbd>. */
export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex items-center rounded-gtc border border-gtc-line bg-gtc-inset px-1.5 py-px",
        "font-mono text-[0.65rem] text-gtc-muted",
        className
      )}
    >
      {children}
    </kbd>
  );
}
