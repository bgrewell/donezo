import { Button, SectionLabel, cn } from "@grewelltech/console";

import { useAppDispatch } from "@/state/AppStore";
import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import type { InterruptedRow } from "./useFocusData";
import { HAIRLINE_ROW } from "./shared";

/** RECENTLY INTERRUPTED — active projects gone quiet for 3–14 days. */
export function InterruptedSection({ rows }: { rows: InterruptedRow[] }) {
  const dispatch = useAppDispatch();
  return (
    <section>
      <SectionLabel className="mb-1 mt-0">Recently interrupted</SectionLabel>
      <ul>
        {rows.map(({ project, latest }) => (
          <li
            key={project.id}
            className={cn("flex items-center gap-3 py-2", HAIRLINE_ROW)}
          >
            <ProjectMark color={project.color} size={7} />
            <span className="shrink-0 font-mono text-[0.72rem] font-medium uppercase tracking-chrome text-gtc-text">
              {project.name}
            </span>
            <span className="min-w-0 flex-1 truncate font-sans text-[0.8rem] text-gtc-muted">
              last: {latest.title}
            </span>
            <span className="shrink-0 font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted">
              {relativeFromToday(latest.date)}
            </span>
            <Button
              size="sm"
              onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: project.id })}
            >
              Resume
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
