import { Select } from "@grewelltech/console";

import type { Project } from "@/domain/types";

/** Compact project selector shared by the tailored field rows. Lists open
 *  (non-completed) projects; `required` swaps the empty option's label so
 *  the kind's constraint reads in place (activity needs a project). The
 *  required prompt is deliberately short — the activity row leaves this
 *  select ~150px wide, and a clipped prompt reads as a glitch.
 *
 *  `emptyLabel` overrides the empty option's text — the activity row uses
 *  "Miscellaneous", since an activity with no project chosen is filed under the
 *  space's catch-all rather than rejected. The catch-all is otherwise a normal
 *  project and appears in the list like any other (you can file tasks, notes
 *  and reminders into it too). */
export function ProjectSelect({
  projects,
  value,
  onChange,
  required = false,
  emptyLabel,
  hideCatchall = false,
  className,
}: {
  projects: Project[];
  value: string;
  onChange: (projectId: string) => void;
  required?: boolean;
  emptyLabel?: string;
  /** Drop the catch-all from the list. The activity row sets this because it
   *  reaches the catch-all through its "Miscellaneous" empty option, so also
   *  listing it would name the same project twice. */
  hideCatchall?: boolean;
  className?: string;
}) {
  const label = emptyLabel ?? (required ? "Pick project…" : "No project");
  return (
    <Select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-label="Project"
      className={className ?? "!py-1.5 !text-[0.75rem]"}
    >
      <option value="">{label}</option>
      {projects
        .filter((p) => !hideCatchall || !p.catchall)
        .map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
    </Select>
  );
}
