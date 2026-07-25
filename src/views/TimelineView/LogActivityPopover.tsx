import * as React from "react";
import { Button, Field, Input, Select } from "@grewelltech/aether";

import type { ActivityType, Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { newId } from "@/lib/id";
import { ProjectMark } from "@/components/common/ProjectMark";
import { ACTIVITY_TYPES, ACTIVITY_TYPE_IDS } from "@/components/common/activityTypes";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/Popover";
import type { CreateDraft } from "./TimelineRow";

/** Click-to-create popover anchored at the clicked spot on a row surface. */
export function LogActivityPopover({
  draft,
  project,
  railWidthExpr,
  headerHeight,
  rowHeight,
  onClose,
}: {
  draft: CreateDraft;
  project: Project;
  railWidthExpr: string;
  headerHeight: number;
  rowHeight: number;
  onClose: () => void;
}) {
  const dispatch = useAppDispatch();
  const [title, setTitle] = React.useState("");
  const [type, setType] = React.useState<ActivityType>("work");
  const [date, setDate] = React.useState(draft.dateISO);
  const [effort, setEffort] = React.useState("");
  const [details, setDetails] = React.useState("");

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = title.trim();
    if (!trimmed || !date) return;
    const id = newId("act");
    dispatch({
      type: "ADD_ACTIVITY",
      entry: {
        id,
        projectId: project.id,
        date,
        type,
        title: trimmed,
        details: details.trim(),
        effortHours: effort ? Number(effort) : undefined,
        source: "manual",
        tags: [],
        links: [],
        planned: false,
      },
    });
    onClose();
    dispatch({ type: "SELECT_ACTIVITY", id });
  };

  return (
    <Popover
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <PopoverAnchor asChild>
        <span
          aria-hidden
          style={{
            position: "absolute",
            left: `calc(${railWidthExpr} + ${draft.xInRow}px)`,
            top: headerHeight + draft.rowIndex * rowHeight + draft.yInRow,
            width: 0,
            height: 0,
          }}
        />
      </PopoverAnchor>
      <PopoverContent align="start" collisionPadding={12} className="w-80">
        <form onSubmit={submit} className="flex flex-col gap-2.5">
          <div className="flex items-center justify-between gap-2">
            <span className="font-mono text-[0.66rem] uppercase tracking-label text-ae-muted">
              Log activity
            </span>
            <span className="flex min-w-0 items-center gap-1.5">
              <ProjectMark color={project.color} size={7} />
              <span className="truncate font-sans text-[0.78rem] text-ae-text">
                {project.name}
              </span>
            </span>
          </div>
          <Field label="Title" htmlFor="dz-log-title">
            <Input
              id="dz-log-title"
              autoFocus
              required
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="What happened?"
              className="px-2 py-1.5 text-[0.8rem]"
            />
          </Field>
          <div className="grid grid-cols-2 gap-2.5">
            <Field label="Type" htmlFor="dz-log-type">
              <Select
                id="dz-log-type"
                value={type}
                onChange={(e) => setType(e.target.value as ActivityType)}
                className="px-2 py-1.5 pr-8 text-[0.8rem]"
              >
                {ACTIVITY_TYPE_IDS.map((t) => (
                  <option key={t} value={t}>
                    {ACTIVITY_TYPES[t].label}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Date" htmlFor="dz-log-date">
              <Input
                id="dz-log-date"
                type="date"
                required
                value={date}
                onChange={(e) => setDate(e.target.value)}
                className="px-2 py-1.5 text-[0.8rem]"
              />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-2.5">
            <Field label="Effort (h)" htmlFor="dz-log-effort">
              <Input
                id="dz-log-effort"
                type="number"
                step={0.5}
                min={0}
                value={effort}
                onChange={(e) => setEffort(e.target.value)}
                placeholder="—"
                className="px-2 py-1.5 text-[0.8rem]"
              />
            </Field>
          </div>
          <Field label="Details" htmlFor="dz-log-details">
            <textarea
              id="dz-log-details"
              rows={2}
              value={details}
              onChange={(e) => setDetails(e.target.value)}
              placeholder="Optional context"
              className="w-full resize-none rounded-ae border border-ae-line bg-ae-inset px-2 py-1.5 font-sans text-[0.85rem] text-ae-text outline-none transition-shadow placeholder:text-ae-muted/70 focus:border-ae-accent focus:shadow-ae-focus"
            />
          </Field>
          <div className="flex justify-end gap-1.5 pt-0.5">
            <Button type="button" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm" variant="primary">
              Log activity
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}
