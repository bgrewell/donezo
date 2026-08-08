import * as React from "react";
import { Input } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { ProjectSelect } from "./ProjectSelect";
import { QuietLabel } from "./chips";

/** Tailored fields for a TASK capture: project + optional due date. */
export function TaskFields({
  projects,
  projectId,
  onProjectId,
  due,
  onDue,
}: {
  projects: Project[];
  projectId: string;
  onProjectId: (id: string) => void;
  /** ISO yyyy-MM-dd; empty string = no due date. */
  due: string;
  onDue: (due: string) => void;
}) {
  // Generated, not fixed: quick capture is no longer the only mount — a note
  // being converted renders these too, and two of them on one page would
  // point every label at the first input.
  const dueId = React.useId();
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="min-w-[11rem] flex-1">
        <ProjectSelect projects={projects} value={projectId} onChange={onProjectId} />
      </div>
      <span className="flex shrink-0 items-center gap-2">
        <QuietLabel htmlFor={dueId}>Due</QuietLabel>
        <Input
          id={dueId}
          type="date"
          value={due}
          onChange={(e) => onDue(e.target.value)}
          className="!w-[9.5rem] !py-1.5 !text-[0.75rem]"
        />
      </span>
    </div>
  );
}
