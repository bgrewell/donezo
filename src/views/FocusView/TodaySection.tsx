import { Button, SectionLabel, cn } from "@grewelltech/console";

import { useAppDispatch } from "@/state/AppStore";
import { ACTIVITY_TYPES, ActivityTypeIcon } from "@/components/common/activityTypes";
import { ProjectMark } from "@/components/common/ProjectMark";
import type { TodayRow } from "./useFocusData";
import { HAIRLINE_ROW, formatApproxHours, formatHours } from "./shared";

/** TODAY — activities logged today, with total effort in the label. */
export function TodaySection({
  rows,
  totalHours,
}: {
  rows: TodayRow[];
  totalHours: number;
}) {
  const dispatch = useAppDispatch();
  return (
    <section>
      <SectionLabel
        className="mb-1 mt-0"
        trailing={
          totalHours > 0 ? (
            <span className="normal-case">{formatApproxHours(totalHours)}</span>
          ) : undefined
        }
      >
        Today
      </SectionLabel>
      {rows.length === 0 ? (
        <div className="flex flex-wrap items-center gap-3 py-1">
          <p className="font-sans text-[0.85rem] text-gtc-muted">Nothing logged yet today.</p>
          <Button size="sm" onClick={() => dispatch({ type: "SET_QUICK_CAPTURE", open: true })}>
            Capture something
          </Button>
        </div>
      ) : (
        <ul>
          {rows.map(({ entry, project }) => (
            <li key={entry.id} className={cn("flex items-center gap-3 py-2", HAIRLINE_ROW)}>
              <span
                className="shrink-0 text-gtc-muted"
                title={ACTIVITY_TYPES[entry.type].label}
              >
                <ActivityTypeIcon type={entry.type} />
              </span>
              <span className="min-w-0 flex-1 truncate font-sans text-[0.85rem] text-gtc-text">
                {entry.title}
              </span>
              {entry.effortHours != null && (
                <span className="shrink-0 font-mono text-[0.66rem] text-gtc-muted">
                  {formatHours(entry.effortHours)}
                </span>
              )}
              {project && (
                <span className="flex shrink-0 items-center" title={project.name}>
                  <ProjectMark color={project.color} size={7} />
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
