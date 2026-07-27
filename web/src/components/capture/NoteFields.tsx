import type { Project } from "@/domain/types";
import { ProjectSelect } from "./ProjectSelect";

/** Tailored fields for a NOTE capture: just an optional project. */
export function NoteFields({
  projects,
  projectId,
  onProjectId,
}: {
  projects: Project[];
  projectId: string;
  onProjectId: (id: string) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="min-w-[11rem] flex-1">
        <ProjectSelect projects={projects} value={projectId} onChange={onProjectId} />
      </div>
    </div>
  );
}
