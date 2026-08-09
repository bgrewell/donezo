import * as React from "react";
import { Input } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { addDaysISO, startOfWeekISO, todayISO } from "@/lib/time";
import { ProjectSelect } from "./ProjectSelect";
import { Chip, QuietLabel } from "./chips";

/** Quick-pick ids for the WHEN row. */
export type WhenChipId = "tomorrow" | "monday" | "nextweek";

/** datetime-local value (yyyy-MM-ddTHH:mm) for a WHEN quick pick. */
export function whenChipValue(id: WhenChipId): string {
  const today = todayISO();
  switch (id) {
    case "tomorrow":
      return `${addDaysISO(today, 1)}T09:00`;
    case "monday":
      // Monday-start weeks: the Monday after this week's, so "Monday 9am"
      // on a Monday still means next Monday.
      return `${addDaysISO(startOfWeekISO(today), 7)}T09:00`;
    case "nextweek":
      return `${addDaysISO(today, 7)}T09:00`;
  }
}

/** Default reminder time for a fresh capture: tomorrow 9am. */
export function defaultRemindAt(): string {
  return whenChipValue("tomorrow");
}

const WHEN_CHIPS: { id: WhenChipId; label: string }[] = [
  { id: "tomorrow", label: "Tomorrow 9am" },
  { id: "monday", label: "Monday 9am" },
  { id: "nextweek", label: "Next week" },
];

/** Tailored fields for a REMINDER capture: WHEN quick chips + a custom
 *  datetime + optional project. Chips fill the custom input; editing the
 *  input hands control back (deselects the chips). */
export function ReminderFields({
  projects,
  projectId,
  onProjectId,
  whenChip,
  remindAt,
  onWhen,
}: {
  projects: Project[];
  projectId: string;
  onProjectId: (id: string) => void;
  /** Selected quick chip, or null when the custom input was edited. */
  whenChip: WhenChipId | null;
  /** datetime-local value (yyyy-MM-ddTHH:mm). */
  remindAt: string;
  onWhen: (chip: WhenChipId | null, remindAt: string) => void;
}) {
  // Generated, not fixed: see TaskFields — these mount outside quick capture
  // now, and a duplicate id would misdirect the label.
  const remindAtId = React.useId();
  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        <QuietLabel htmlFor={remindAtId}>When</QuietLabel>
        {WHEN_CHIPS.map((c) => (
          <Chip
            key={c.id}
            selected={whenChip === c.id}
            onClick={() => onWhen(c.id, whenChipValue(c.id))}
          >
            {c.label}
          </Chip>
        ))}
        {/* 12.25rem: the extra quarter-rem keeps the value clear of the
            calendar glyph at the large text size without wrapping the WHEN
            row at desktop width / medium text. */}
        <Input
          id={remindAtId}
          type="datetime-local"
          value={remindAt}
          onChange={(e) => onWhen(null, e.target.value)}
          className="!w-[12.25rem] !py-1.5 !text-[0.75rem]"
        />
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
        <div className="min-w-[11rem] flex-1">
          <ProjectSelect projects={projects} value={projectId} onChange={onProjectId} />
        </div>
      </div>
    </div>
  );
}
