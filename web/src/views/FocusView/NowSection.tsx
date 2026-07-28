import { SectionLabel } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";

/** NOW — the current thread: most recently touched active project. */
export function NowSection({
  project,
  lastTouched,
}: {
  project?: Project;
  lastTouched?: string;
}) {
  const dispatch = useAppDispatch();
  return (
    <section>
      <SectionLabel className="mb-2.5 mt-0">Now</SectionLabel>
      {!project ? (
        <p className="font-sans text-[0.85rem] text-gtc-muted">
          No active projects. Start one from Projects.
        </p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <ProjectMark color={project.color} size={9} />
            <button
              type="button"
              onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: project.id })}
              className="rounded-gtc font-mono text-[0.88rem] font-semibold uppercase tracking-chrome text-gtc-text transition-colors hover:text-gtc-accent-bright focus-visible:outline-none focus-visible:shadow-gtc-focus"
            >
              {project.name}
            </button>
            <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
              {project.status} · last touched{" "}
              {lastTouched ? relativeFromToday(lastTouched) : "never"}
            </span>
          </div>
          <div className="mt-2.5 border-l-2 border-gtc-accent bg-gtc-tint-accent px-3 py-2">
            <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-accent">
              Resume here
            </div>
            <p className="mt-1 max-w-[68ch] font-sans text-[0.85rem] leading-relaxed text-gtc-text">
              {project.resumeContext}
            </p>
          </div>
        </>
      )}
    </section>
  );
}
