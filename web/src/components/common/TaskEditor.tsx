import * as React from "react";
import { Button, Input, cn } from "@grewelltech/console";

import type { TaskItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { ProjectSelect } from "@/components/capture/ProjectSelect";

/** Inline editor for a task: title, details, due date, and project.
 *
 *  Extracted from ProjectDetail for #29, which asks for a shared row
 *  affordance rather than a copy per surface — a task is now edited the same
 *  way in Focus as in its project, from one implementation.
 *
 *  Title and details sit together because the move this exists for is usually
 *  cutting one into the other: before #44 a task had nowhere to put the long
 *  form, so it went in the title. */
export function TaskEditor({ task, onDone }: { task: TaskItem; onDone: () => void }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const [title, setTitle] = React.useState(task.title);
  const [details, setDetails] = React.useState(task.details);
  const [due, setDue] = React.useState(task.due ?? "");
  const [projectId, setProjectId] = React.useState(task.projectId ?? "");
  // What the task looked like when this editor opened. The patch is diffed
  // against THIS, not against the live prop, and the difference is not
  // academic: the prop moves under an open editor every time the freshness
  // poll applies REPLACE_STATE, so diffing against it would put fields the
  // user never touched into the patch and quietly overwrite whatever changed
  // them — an agent's write, or the same task edited from another row.
  const seeded = React.useRef({
    title: task.title,
    details: task.details,
    due: task.due,
    projectId: task.projectId,
  });

  // Same rule as editing a note: a closed project stays listed while the task
  // is on it, so editing anything else cannot silently move it off.
  const projects = state.projects.filter((p) => !isClosedProject(p) || p.id === task.projectId);

  const trimmed = title.trim();
  const save = () => {
    if (!trimmed) return;
    const was = seeded.current;
    const patch: Partial<TaskItem> = {};
    if (trimmed !== was.title) patch.title = trimmed;
    if (details !== was.details) patch.details = details;
    if ((due || undefined) !== was.due) patch.due = due || undefined;
    if ((projectId || undefined) !== was.projectId) patch.projectId = projectId || undefined;
    if (Object.keys(patch).length > 0) {
      dispatch({ type: "UPDATE_TASK", id: task.id, patch });
    }
    onDone();
  };

  return (
    <div className="w-full space-y-2 py-1">
      <Input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        aria-label="Task title"
        className="!font-sans !text-[0.9rem] normal-case"
      />
      <textarea
        value={details}
        onChange={(e) => setDetails(e.target.value)}
        aria-label="Task details"
        rows={4}
        placeholder="Anything too long for the title."
        className={cn(
          "w-full rounded-gtc border border-gtc-line bg-gtc-inset px-2 py-1.5",
          "font-sans text-[0.8rem] text-gtc-text placeholder:text-gtc-muted",
          "focus:border-gtc-accent focus:outline-none"
        )}
      />
      <div className="flex flex-wrap items-center gap-2">
        <span className="flex items-center gap-2">
          <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
            Due
          </span>
          <Input
            type="date"
            value={due}
            onChange={(e) => setDue(e.target.value)}
            aria-label="Task due date"
            className="!w-[9.5rem] !py-1.5 !text-[0.75rem]"
          />
        </span>
        <div className="min-w-[11rem]">
          <ProjectSelect projects={projects} value={projectId} onChange={setProjectId} />
        </div>
        <Button size="sm" variant="primary" onClick={save} disabled={!trimmed}>
          Save
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={onDone}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
