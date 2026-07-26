import { Button } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { ProjectMark } from "@/components/common/ProjectMark";

/** NEXT ACTION — the single highlighted next step for the current thread. */
export function NextActionPanel({ project }: { project?: Project }) {
  const dispatch = useAppDispatch();
  if (!project) return null;
  const alts = project.altNextActions.slice(0, 2);
  return (
    <section>
      <div data-tour="next-action" className="rounded-gtc border border-gtc-line bg-gtc-panel bg-gtc-sheen px-4 py-3">
        <div className="font-mono text-[0.64rem] uppercase tracking-label text-gtc-accent">
          Next action
        </div>
        <p className="mt-1.5 max-w-[68ch] font-sans text-[0.95rem] leading-relaxed text-gtc-text">
          {project.nextAction}
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: project.id })}
          >
            Open project
          </Button>
          <Button
            size="sm"
            onClick={() => dispatch({ type: "SET_QUICK_CAPTURE", open: true })}
          >
            Log progress
          </Button>
        </div>
      </div>
      {alts.length > 0 && (
        <div className="mt-2.5 space-y-1.5 px-1">
          <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            Or
          </div>
          {alts.map((alt) => (
            <div key={alt} className="flex items-center gap-2">
              <ProjectMark color={project.color} size={6} muted />
              <span className="font-sans text-[0.8rem] text-gtc-muted">{alt}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
