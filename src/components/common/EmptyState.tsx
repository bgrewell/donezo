import type { ReactNode } from "react";
import { cn } from "@grewelltech/aether";

/** Calm empty/placeholder state — partial organization is normal here. */
export function EmptyState({
  title,
  hint,
  children,
  className,
}: {
  title: string;
  hint?: string;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2 border border-dashed border-ae-line px-6 py-10 text-center",
        className
      )}
    >
      <div className="font-mono text-[0.72rem] uppercase tracking-chrome text-ae-muted">
        {title}
      </div>
      {hint && <p className="max-w-[42ch] text-[0.85rem] text-ae-muted">{hint}</p>}
      {children}
    </div>
  );
}
