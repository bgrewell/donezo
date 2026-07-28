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
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="min-w-[11rem] flex-1">
        <ProjectSelect projects={projects} value={projectId} onChange={onProjectId} />
      </div>
      <span className="flex shrink-0 items-center gap-2">
        <QuietLabel htmlFor="qc-task-due">Due</QuietLabel>
        <Input
          id="qc-task-due"
          type="date"
          value={due}
          onChange={(e) => onDue(e.target.value)}
          className="!w-[9.5rem] !py-1.5 !text-[0.75rem]"
        />
      </span>
    </div>
  );
}
