import * as React from "react";
import { Button } from "@grewelltech/console";

import type { ActivityType, NoteItem, NoteTargetKind } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { newId } from "@/lib/id";
import { todayISO, withSeconds } from "@/lib/time";
import { ActivityFields } from "@/components/capture/ActivityFields";
import {
  ReminderFields,
  defaultRemindAt,
  type WhenChipId,
} from "@/components/capture/ReminderFields";
import { TaskFields } from "@/components/capture/TaskFields";
import { Chip } from "@/components/capture/chips";

/** What a note may become. Deliberately three of the five capture kinds:
 *  note-to-note is what Edit is for, and note-to-project is not a sensible
 *  target — a note is content, not a stream of work. */
const TARGETS: NoteTargetKind[] = ["task", "reminder", "activity"];

/** Plain-language summary of what pressing Convert will do. The note is
 *  destroyed, so the panel says so before the fact rather than leaving the
 *  user to discover it from a vanished row. */
function outcome(kind: NoteTargetKind, hasBody: boolean): string {
  // Only "activity" takes "an" — the two other targets are consonant-initial.
  const article = kind === "activity" ? "an" : "a";
  const keeps =
    kind === "activity"
      ? " Its body is kept as the activity's details."
      : hasBody
        ? ` Its body is not kept — a ${kind} has nowhere to put it.`
        : "";
  return `This note becomes ${article} ${kind} and is removed.${keeps}`;
}

/** Convert one note into a task, reminder, or activity.
 *
 *  The tailored field rows are quick capture's, so the fields for a kind
 *  look and behave the same wherever you meet them. Everything defaults
 *  from the note: its title becomes the new item's title (or a reminder's
 *  text) and its project carries over, so the common case is two clicks. */
export function ConvertNotePanel({ note, onDone }: { note: NoteItem; onDone: () => void }) {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const [kind, setKind] = React.useState<NoteTargetKind>("task");
  const [projectId, setProjectId] = React.useState(note.projectId ?? "");
  const [due, setDue] = React.useState("");
  const [whenChip, setWhenChip] = React.useState<WhenChipId | null>("tomorrow");
  const [remindAt, setRemindAt] = React.useState(defaultRemindAt);
  const [activityType, setActivityType] = React.useState<ActivityType>("work");
  const [activityDate, setActivityDate] = React.useState(todayISO);
  const [effort, setEffort] = React.useState("");

  // Same rule as editing a note: a closed project stays listed while the
  // note is on it, so converting does not silently move the item off it.
  const projects = state.projects.filter((p) => !isClosedProject(p) || p.id === note.projectId);
  const needsProject = kind === "activity" && !projectId;
  const blocked = needsProject || (kind === "reminder" && !remindAt);

  const convert = () => {
    if (blocked) return;
    const base = { type: "CONVERT_NOTE" as const, id: note.id, kind };
    switch (kind) {
      case "task":
        dispatch({
          ...base,
          task: {
            id: newId("tsk"),
            projectId: projectId || undefined,
            title: note.title,
            status: "open",
            due: due || undefined,
            createdAt: todayISO(),
          },
        });
        break;
      case "reminder":
        dispatch({
          ...base,
          reminder: {
            id: newId("rem"),
            text: note.title,
            remindAt: withSeconds(remindAt),
            projectId: projectId || undefined,
          },
        });
        break;
      case "activity": {
        if (!projectId) return;
        const hours = Number(effort);
        dispatch({
          ...base,
          activity: {
            id: newId("act"),
            projectId,
            date: activityDate || todayISO(),
            type: activityType,
            title: note.title,
            // The one target with somewhere to keep the body.
            details: note.body,
            effortHours:
              effort.trim() !== "" && Number.isFinite(hours) && hours > 0 ? hours : undefined,
            // Not "capture": this was written as a note and reclassified by
            // hand, not swept up by the capture buffer.
            source: "manual",
            tags: [],
            links: [],
          },
        });
        break;
      }
    }
    onDone();
  };

  return (
    <div className="mt-2 space-y-2.5 rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Convert to">
        {TARGETS.map((k) => (
          <Chip key={k} selected={kind === k} onClick={() => setKind(k)}>
            {k}
          </Chip>
        ))}
      </div>

      {kind === "task" && (
        <TaskFields
          projects={projects}
          projectId={projectId}
          onProjectId={setProjectId}
          due={due}
          onDue={setDue}
        />
      )}
      {kind === "reminder" && (
        <ReminderFields
          projects={projects}
          projectId={projectId}
          onProjectId={setProjectId}
          whenChip={whenChip}
          remindAt={remindAt}
          onWhen={(chip, at) => {
            setWhenChip(chip);
            setRemindAt(at);
          }}
        />
      )}
      {kind === "activity" && (
        <ActivityFields
          projects={projects}
          projectId={projectId}
          onProjectId={setProjectId}
          type={activityType}
          onType={setActivityType}
          date={activityDate}
          onDate={setActivityDate}
          effort={effort}
          onEffort={setEffort}
        />
      )}

      <p className="font-sans text-[0.75rem] text-gtc-muted">
        {outcome(kind, note.body.trim() !== "")}
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="primary" onClick={convert} disabled={blocked}>
          Convert
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={onDone}>
          Cancel
        </Button>
        {needsProject && (
          <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            Needs a project
          </span>
        )}
      </div>
    </div>
  );
}
