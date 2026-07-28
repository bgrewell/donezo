import { Select } from "@grewelltech/console";

import type { Project } from "@/domain/types";

/** Compact project selector shared by the tailored field rows. Lists open
 *  (non-completed) projects; `required` swaps the empty option's label so
 *  the kind's constraint reads in place (activity needs a project). The
 *  required prompt is deliberately short — the activity row leaves this
 *  select ~150px wide, and a clipped prompt reads as a glitch. */
export function ProjectSelect({
  projects,
  value,
  onChange,
  required = false,
  className,
}: {
  projects: Project[];
  value: string;
  onChange: (projectId: string) => void;
  required?: boolean;
  className?: string;
}) {
  return (
    <Select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-label="Project"
      className={className ?? "!py-1.5 !text-[0.75rem]"}
    >
      <option value="">{required ? "Pick project…" : "No project"}</option>
      {projects.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </Select>
  );
}
