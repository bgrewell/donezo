import * as React from "react";
import { SectionLabel, cn } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";
import { RowActions } from "@/components/common/RowActions";
import { DetailsDisclosure } from "@/components/common/DetailsDisclosure";
import { TaskEditor } from "@/components/common/TaskEditor";
import type { WaitingTaskRow } from "./useFocusData";
import { HAIRLINE_ROW } from "./shared";

/** WAITING ON — tasks and projects blocked on someone or something else.
 *
 *  A waiting task is exactly the kind #29 is about: the thing you want when
 *  you see one is to unblock it or chase it, and both meant going elsewhere.
 *  Unblocking is the action worth one click — it is the state change that
 *  moves a task back into play. */
export function WaitingSection({
  tasks,
  projects,
}: {
  tasks: WaitingTaskRow[];
  projects: Project[];
}) {
  const dispatch = useAppDispatch();
  const [editing, setEditing] = React.useState<string | null>(null);
  const rowClass = cn(
    "flex flex-wrap items-center gap-x-3 gap-y-1 py-2 transition-colors hover:bg-gtc-tint-accent",
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
        {tasks.map(({ task, project }) =>
          editing === task.id ? (
            <li key={task.id} className={cn("py-2", HAIRLINE_ROW)}>
              <TaskEditor task={task} onDone={() => setEditing(null)} />
            </li>
          ) : (
          <li key={task.id} className={cn("group", rowClass)}>
            <span className="min-w-0 grow basis-full truncate font-sans text-[0.85rem] text-gtc-text sm:basis-0">
              {task.title}
            </span>
            {/* Meta + mark drop under the title together on phones, where a
                long "waiting on …" must wrap (min-w-0, shrink allowed) —
                sm+ keeps the single unshrunk line. */}
            <span className="flex min-w-0 items-center gap-3 sm:shrink-0">
              <span className="min-w-0 font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted">
                waiting on {task.waitingOn ?? "—"}
              </span>
              {project && <ProjectMark color={project.color} size={7} />}
            </span>
            <RowActions
              label={`Actions for ${task.title}`}
              actions={[
                {
                  label: "Unblock",
                  onSelect: () =>
                    dispatch({
                      type: "UPDATE_TASK",
                      id: task.id,
                      patch: { status: "open", waitingOn: undefined },
                    }),
                },
                { label: "Edit", onSelect: () => setEditing(task.id) },
              ]}
            />
            {task.details && (
              <span className="basis-full">
                <DetailsDisclosure details={task.details} />
              </span>
            )}
          </li>
          )
        )}
        {projects.map((p) => (
          <li key={p.id} className={rowClass}>
            <ProjectMark color={p.color} size={7} />
            <button
              type="button"
              onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: p.id })}
              className="shrink-0 rounded-gtc font-mono text-[0.72rem] font-medium uppercase tracking-chrome text-gtc-text transition-colors hover:text-gtc-accent-bright focus-visible:outline-none focus-visible:shadow-gtc-focus"
            >
              {p.name}
            </button>
            <StatusBadge status={p.status} />
            <span className="min-w-0 grow basis-full truncate font-sans text-[0.8rem] text-gtc-muted sm:basis-0">
              {p.waitingOn}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
