import type { ProjectColor } from "@/domain/types";

/** CSS variable reference for a project color, e.g. var(--dz-pj-blue). */
export function projectColorVar(color: ProjectColor): string {
  return `var(--dz-pj-${color})`;
}
