import * as React from "react";
import { Button, Input, cn } from "@grewelltech/console";

import type { Reminder } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { withSeconds } from "@/lib/time";
import { ProjectSelect } from "@/components/capture/ProjectSelect";

/** Inline editor for a reminder: text, details, when, and project.
 *
 *  #44 gave reminders a details field everywhere except the app, because
 *  nowhere listed them with room to edit one — that was left to #29, and this
 *  is it. Reminders are now editable wherever they are listed.
 *
 *  Follows the shape NoteRow established: a draft seeded from the item and
 *  discarded on cancel, so every editable surface behaves the same way. */
export function ReminderEditor({
  reminder,
  onDone,
}: {
  reminder: Reminder;
  onDone: () => void;
}) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const [text, setText] = React.useState(reminder.text);
  const [details, setDetails] = React.useState(reminder.details);
  // datetime-local wants no seconds; the API stores them.
  const [remindAt, setRemindAt] = React.useState(reminder.remindAt.slice(0, 16));
  const [projectId, setProjectId] = React.useState(reminder.projectId ?? "");
  // See TaskEditor: diffed against what the editor opened on, so a field the
  // user never touched cannot enter the patch and revert someone else's
  // change.
  const seeded = React.useRef({
    text: reminder.text,
    details: reminder.details,
    remindAt: reminder.remindAt,
    projectId: reminder.projectId,
  });

  // Same rule as editing a note: a closed project stays listed while the item
  // is on it, so editing anything else cannot silently move it off.
  const projects = state.projects.filter(
    (p) => !isClosedProject(p) || p.id === reminder.projectId
  );

  const trimmed = text.trim();
  const save = () => {
    if (!trimmed || !remindAt) return;
    const was = seeded.current;
    const patch: Partial<Reminder> = {};
    if (trimmed !== was.text) patch.text = trimmed;
    if (details !== was.details) patch.details = details;
    if (withSeconds(remindAt) !== was.remindAt) patch.remindAt = withSeconds(remindAt);
    if ((projectId || undefined) !== was.projectId) {
      patch.projectId = projectId || undefined;
    }
    if (Object.keys(patch).length > 0) {
      dispatch({ type: "UPDATE_REMINDER", id: reminder.id, patch });
    }
    onDone();
  };

  return (
    <div className="w-full space-y-2 py-1">
      <Input
        value={text}
        onChange={(e) => setText(e.target.value)}
        aria-label="Reminder text"
        className="!font-sans !text-[0.85rem] normal-case"
      />
      <textarea
        value={details}
        onChange={(e) => setDetails(e.target.value)}
        aria-label="Reminder details"
        rows={3}
        placeholder="Anything too long for one line."
        className={cn(
          "w-full rounded-gtc border border-gtc-line bg-gtc-inset px-2 py-1.5",
          "font-sans text-[0.8rem] text-gtc-text placeholder:text-gtc-muted",
          "focus:border-gtc-accent focus:outline-none"
        )}
      />
      <div className="flex flex-wrap items-center gap-2">
        <Input
          type="datetime-local"
          value={remindAt}
          onChange={(e) => setRemindAt(e.target.value)}
          aria-label="Remind at"
          className="!w-[12.25rem] !py-1.5 !text-[0.75rem]"
        />
        <div className="min-w-[11rem]">
          <ProjectSelect projects={projects} value={projectId} onChange={setProjectId} />
        </div>
        <Button size="sm" variant="primary" onClick={save} disabled={!trimmed || !remindAt}>
          Save
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={onDone}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
