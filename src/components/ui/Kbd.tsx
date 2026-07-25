import type { ReactNode } from "react";
import { cn } from "@grewelltech/aether";

/** Compact keyboard-shortcut chip, e.g. <Kbd>⌘K</Kbd>. */
export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex items-center rounded-ae border border-ae-line bg-ae-inset px-1.5 py-px",
        "font-mono text-[0.65rem] text-ae-muted",
        className
      )}
    >
      {children}
    </kbd>
  );
}
