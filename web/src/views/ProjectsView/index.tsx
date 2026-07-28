import { useAppState } from "@/state/AppStore";
import { projectById } from "@/state/selectors";
import { ProjectList } from "./ProjectList";
import { ProjectDetail } from "./ProjectDetail";

/** Projects: master list, or a single project's detail when one is open. */
export default function ProjectsView() {
  const state = useAppState();
  const project = projectById(state, state.selectedProjectId);
  return (
    <div className="h-full overflow-y-auto">
      {project ? <ProjectDetail project={project} /> : <ProjectList />}
    </div>
  );
}
