import { cn } from "@grewelltech/console";

import type { Project, ProjectStatus } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { latestActivityDate, openTaskCount } from "@/state/selectors";
import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import { Tip } from "@/components/ui/Tooltip";
import { railWidth } from "./geometry";

const STATUS_TEXT: Record<ProjectStatus, { word: string; cls: string }> = {
  active: { word: "active", cls: "text-gtc-accent" },
  waiting: { word: "waiting", cls: "text-gtc-warn" },
  blocked: { word: "blocked", cls: "text-gtc-danger" },
  paused: { word: "paused", cls: "text-gtc-muted" },
  completed: { word: "done", cls: "text-gtc-success" },
};

/** Sticky left rail cell for a project row. The whole cell opens the project. */
export function ProjectRailCell({
  project,
  railCollapsed,
  showFocus,
}: {
  project: Project;
  railCollapsed: boolean;
  /** Third line (current focus) — only when the row is tall enough. */
  showFocus: boolean;
}) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const latest = latestActivityDate(state, project.id);
  const tasks = openTaskCount(state, project.id);
  const status = STATUS_TEXT[project.status];

  const open = () => dispatch({ type: "OPEN_PROJECT", projectId: project.id });

  if (railCollapsed) {
    return (
      <Tip content={project.name} side="right">
        <button
          type="button"
          onClick={open}
          aria-label={`Open project ${project.name}`}
          className="group sticky left-0 z-20 flex w-[44px] shrink-0 items-center justify-center border-r border-gtc-line bg-gtc-panel outline-none focus-visible:shadow-gtc-focus"
        >
          <span
            aria-hidden
            className="absolute inset-0 bg-gtc-tint-accent opacity-0 transition-opacity group-hover:opacity-100"
          />
          <ProjectMark
            color={project.color}
            size={10}
            muted={project.status === "completed"}
            className="relative"
          />
        </button>
      </Tip>
    );
  }

  return (
    <button
      type="button"
      onClick={open}
      data-tour="rail"
      className="group sticky left-0 z-20 flex shrink-0 flex-col justify-center gap-0.5 overflow-hidden border-r border-gtc-line bg-gtc-panel px-2.5 text-left outline-none focus-visible:shadow-gtc-focus"
      style={{ width: railWidth(false) }}
    >
      <span
        aria-hidden
        className="absolute inset-0 bg-gtc-tint-accent opacity-0 transition-opacity group-hover:opacity-100"
      />
      <span className="relative flex w-full items-center gap-1.5">
        <ProjectMark
          color={project.color}
          size={8}
          muted={project.status === "completed"}
        />
        <span className="min-w-0 flex-1 truncate font-sans text-[0.82rem] font-medium leading-snug text-gtc-text">
          {project.name}
        </span>
        {(project.status === "waiting" || project.status === "blocked") && (
          <Tip content={project.waitingOn ?? status.word}>
            <span
              className={cn(
                "h-1.5 w-1.5 shrink-0",
                project.status === "waiting" ? "bg-gtc-warn" : "bg-gtc-danger"
              )}
            />
          </Tip>
        )}
      </span>
      <span className="relative flex w-full items-center gap-1 overflow-hidden whitespace-nowrap font-mono text-[0.62rem] uppercase leading-snug tracking-label text-gtc-muted">
        <span className={status.cls}>{status.word}</span>
        <span aria-hidden>·</span>
        <span>{latest ? relativeFromToday(latest) : "no entries"}</span>
        <span aria-hidden>·</span>
        <span>
          {tasks} {tasks === 1 ? "task" : "tasks"}
        </span>
      </span>
      {showFocus && project.currentFocus && (
        <span className="relative w-full truncate font-sans text-[0.7rem] leading-snug text-gtc-muted">
          {project.currentFocus}
        </span>
      )}
    </button>
  );
}
