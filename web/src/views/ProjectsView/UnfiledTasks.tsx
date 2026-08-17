import { SectionLabel } from "@grewelltech/console";

import type { TaskItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { ProjectSelect } from "@/components/capture/ProjectSelect";
import { TaskRow } from "./ProjectDetail";

/**
 * Tasks that belong to no project.
 *
 * A task with no project is legitimate — the store allows it, quick capture
 * creates one when no project is picked, and MCP/inbox conversion can too — but
 * until this existed there was nowhere in the app it rendered. Project detail
 * filters strictly by project id and Focus only lists tasks with a due date, so
 * an unfiled, undated task could be found by search and then not acted on. This
 * is the tasks half of #25 (which fixed the same gap for notes).
 *
 * Done tasks are left out — a finished chore needs no home — and the section is
 * shown only when there are some, so an empty "Unfiled" heading never sits on
 * the projects page earning nothing.
 */
export function UnfiledTasks() {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const unfiled = state.tasks.filter((t) => !t.projectId && t.status !== "done");
  if (unfiled.length === 0) return null;

  // Same set the capture rows offer: filing into a finished project is rarely
  // what is meant, and the list stays short.
  const openProjects = state.projects.filter((p) => !isClosedProject(p));

  const file = (task: TaskItem, projectId: string) => {
    if (!projectId) return;
    dispatch({ type: "UPDATE_TASK", id: task.id, patch: { projectId } });
  };

  const complete = (task: TaskItem) => {
    dispatch({ type: "UPDATE_TASK", id: task.id, patch: { status: "done" } });
  };

  return (
    <section className="mt-8">
      <SectionLabel trailing={<span className="text-gtc-text">{unfiled.length}</span>}>
        Unfiled tasks
      </SectionLabel>
      <p className="mb-2 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Tasks that belong to no project yet. Give one a project and it moves to
        that project&rsquo;s page.
      </p>
      <ul className="divide-y divide-gtc-line/60">
        {unfiled.map((task) => (
          <li key={task.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 py-1">
            <div className="min-w-0 flex-1">
              <TaskRow task={task} onDone={() => complete(task)} />
            </div>
            {/* basis-full below sm so the select drops under the task rather
                than crushing the title on a phone. */}
            <div className="flex shrink-0 basis-full items-center pt-2 sm:basis-auto sm:pt-3">
              <ProjectSelect
                projects={openProjects}
                value=""
                onChange={(projectId) => file(task, projectId)}
                required
              />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
