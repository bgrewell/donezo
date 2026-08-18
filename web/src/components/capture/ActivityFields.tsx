import * as React from "react";
import { Input, Select } from "@grewelltech/console";

import type { ActivityType, Project } from "@/domain/types";
import { ACTIVITY_TYPES, ACTIVITY_TYPE_IDS } from "@/components/common/activityTypes";
import { ProjectSelect } from "./ProjectSelect";
import { QuietLabel } from "./chips";

/** Tailored fields for an ACTIVITY capture: project, type, date, and optional
 *  effort hours.
 *
 *  `catchAllFallback` distinguishes the two callers: quick capture sets it, so
 *  an activity with no project is filed under the space's catch-all (the empty
 *  choice reads "Miscellaneous"). Note conversion leaves it off — that path
 *  inserts the activity directly, without catch-all routing, so it still needs
 *  an explicit project and the empty choice stays a "pick one" prompt. */
export function ActivityFields({
  projects,
  projectId,
  onProjectId,
  type,
  onType,
  date,
  onDate,
  effort,
  onEffort,
  catchAllFallback = false,
}: {
  projects: Project[];
  projectId: string;
  onProjectId: (id: string) => void;
  type: ActivityType;
  onType: (t: ActivityType) => void;
  /** ISO yyyy-MM-dd. */
  date: string;
  onDate: (date: string) => void;
  /** Raw hours text; empty = not logged. */
  effort: string;
  onEffort: (effort: string) => void;
  /** When set, no project means the catch-all; otherwise a project is required. */
  catchAllFallback?: boolean;
}) {
  // Generated, not fixed: see TaskFields — these mount outside quick capture
  // now, and a duplicate id would misdirect the label.
  const hoursId = React.useId();
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="min-w-[8.5rem] flex-1">
        {/* With the catch-all fallback the empty choice reads "Miscellaneous"
            (a valid default) and hideCatchall keeps the catch-all out of the
            list, since the empty option already points there. Without it (note
            conversion) the select stays a required "pick a project" prompt. */}
        <ProjectSelect
          projects={projects}
          value={projectId}
          onChange={onProjectId}
          required={!catchAllFallback}
          emptyLabel={catchAllFallback ? "Miscellaneous" : undefined}
          hideCatchall={catchAllFallback}
        />
      </div>
      <div className="w-[6.5rem]">
        <Select
          value={type}
          onChange={(e) => onType(e.target.value as ActivityType)}
          aria-label="Activity type"
          className="!py-1.5 !text-[0.75rem]"
        >
          {ACTIVITY_TYPE_IDS.map((t) => (
            <option key={t} value={t}>
              {ACTIVITY_TYPES[t].label}
            </option>
          ))}
        </Select>
      </div>
      <Input
        type="date"
        value={date}
        onChange={(e) => onDate(e.target.value)}
        aria-label="Activity date"
        className="!w-[8.5rem] !py-1.5 !text-[0.75rem]"
      />
      {/* Label + input wrap together — a lone hours box on the next row
          reads as detached. */}
      <span className="flex shrink-0 items-center gap-2">
        <QuietLabel htmlFor={hoursId}>Hours</QuietLabel>
        <Input
          id={hoursId}
          type="number"
          step={0.5}
          min={0}
          value={effort}
          onChange={(e) => onEffort(e.target.value)}
          className="!w-[4rem] !py-1.5 !text-[0.75rem]"
        />
      </span>
    </div>
  );
}
