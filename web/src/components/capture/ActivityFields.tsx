import * as React from "react";
import { Input, Select } from "@grewelltech/console";

import type { ActivityType, Project } from "@/domain/types";
import { ACTIVITY_TYPES, ACTIVITY_TYPE_IDS } from "@/components/common/activityTypes";
import { ProjectSelect } from "./ProjectSelect";
import { QuietLabel } from "./chips";

/** Tailored fields for an ACTIVITY capture: project (required), type,
 *  date, and optional effort hours. */
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
}) {
  // Generated, not fixed: see TaskFields — these mount outside quick capture
  // now, and a duplicate id would misdirect the label.
  const hoursId = React.useId();
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
      <div className="min-w-[8.5rem] flex-1">
        <ProjectSelect projects={projects} value={projectId} onChange={onProjectId} required />
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
