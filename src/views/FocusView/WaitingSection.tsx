import { SectionLabel, cn } from "@grewelltech/aether";

import type { Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";
import type { WaitingTaskRow } from "./useFocusData";
import { HAIRLINE_ROW } from "./shared";

/** WAITING ON — tasks and projects blocked on someone or something else. */
export function WaitingSection({
  tasks,
  projects,
}: {
  tasks: WaitingTaskRow[];
  projects: Project[];
}) {
  const dispatch = useAppDispatch();
  const rowClass = cn(
    "flex items-center gap-3 py-2 transition-colors hover:bg-ae-tint-accent",
    HAIRLINE_ROW
  );
  return (
    <section>
      <SectionLabel
        className="mb-1 mt-0"
        trailing={<span>{tasks.length + projects.length}</span>}
      >
        Waiting on
      </SectionLabel>
      <ul>
        {tasks.map(({ task, project }) => (
          <li key={task.id} className={rowClass}>
            <span className="min-w-0 flex-1 truncate font-sans text-[0.85rem] text-ae-text">
              {task.title}
            </span>
            <span className="shrink-0 font-mono text-[0.64rem] uppercase tracking-label text-ae-muted">
              waiting on {task.waitingOn ?? "—"}
            </span>
            {project && <ProjectMark color={project.color} size={7} />}
          </li>
        ))}
        {projects.map((p) => (
          <li key={p.id} className={rowClass}>
            <ProjectMark color={p.color} size={7} />
            <button
              type="button"
              onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: p.id })}
              className="shrink-0 rounded-ae font-mono text-[0.72rem] font-medium uppercase tracking-chrome text-ae-text transition-colors hover:text-ae-accent-bright focus-visible:outline-none focus-visible:shadow-ae-focus"
            >
              {p.name}
            </button>
            <StatusBadge status={p.status} />
            <span className="min-w-0 flex-1 truncate font-sans text-[0.8rem] text-ae-muted">
              {p.waitingOn}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
