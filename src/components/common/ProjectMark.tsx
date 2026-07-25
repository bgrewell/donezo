import { cn } from "@grewelltech/aether";
import type { ProjectColor } from "@/domain/types";
import { projectColorVar } from "@/lib/projectColors";

/** Square project color marker (Aether: dots are square, never circular). */
export function ProjectMark({
  color,
  size = 8,
  muted = false,
  className,
}: {
  color: ProjectColor;
  size?: number;
  /** Dimmed rendering for completed/paused contexts. */
  muted?: boolean;
  className?: string;
}) {
  return (
    <span
      aria-hidden
      className={cn("inline-block shrink-0", muted && "opacity-50", className)}
      style={{ width: size, height: size, background: projectColorVar(color) }}
    />
  );
}
