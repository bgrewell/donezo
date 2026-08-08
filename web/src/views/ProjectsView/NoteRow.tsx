import * as React from "react";
import { Button, Input, cn } from "@grewelltech/console";

import type { NoteItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { ProjectSelect } from "@/components/capture/ProjectSelect";
import { ConvertNotePanel } from "./ConvertNotePanel";

/** Body preview for a collapsed note row. */
function excerpt(text: string, max = 100): string {
  return text.length <= max ? text : `${text.slice(0, max).trimEnd()}…`;
}

type Mode = "view" | "edit" | "convert" | "confirm-delete";

/** One note, with in-place edit, conversion, and a two-click delete.
 *
 *  Delete is a plain confirm rather than the typed-name confirmation a
 *  project needs: a note owns nothing, so removing one cannot cascade.
 *  Editing follows the Inspector's shape — a draft seeded from the note,
 *  discarded on cancel — so the two editable surfaces behave the same way.
 *  Convert also removes the note, but it is not a second delete confirm: the
 *  panel is the deliberate step and says what will happen before it does. */
export function NoteRow({ note }: { note: NoteItem }) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const [mode, setMode] = React.useState<Mode>("view");
  const [title, setTitle] = React.useState(note.title);
  const [body, setBody] = React.useState(note.body);
  const [projectId, setProjectId] = React.useState(note.projectId ?? "");

  const startEdit = () => {
    // Re-seed from the note so a previous cancelled edit is not resurrected.
    setTitle(note.title);
    setBody(note.body);
    setProjectId(note.projectId ?? "");
    setMode("edit");
  };

  const trimmedTitle = title.trim();
  const currentProject = note.projectId ?? "";
  const dirty = trimmedTitle !== note.title || body !== note.body || projectId !== currentProject;

  // Unlike an activity, a note may legitimately have no project, so the
  // empty choice detaches it rather than being refused. projectId is a
  // clearable field server-side, so undefined here becomes an explicit null
  // on the wire.
  const save = () => {
    if (!trimmedTitle) return;
    if (dirty) {
      dispatch({
        type: "UPDATE_NOTE",
        id: note.id,
        patch: { title: trimmedTitle, body, projectId: projectId || undefined },
      });
    }
    setMode("view");
  };

  if (mode === "edit") {
    return (
      <div className="space-y-2 py-2">
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          aria-label="Note title"
          className="!font-sans !text-[0.85rem] normal-case"
        />
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          aria-label="Note body"
          rows={4}
          className={cn(
            "w-full rounded-gtc border border-gtc-line bg-gtc-inset px-2 py-1.5",
            "font-sans text-[0.8rem] text-gtc-text",
            "focus:border-gtc-accent focus:outline-none"
          )}
        />
        <div className="flex flex-wrap items-center gap-2">
          <ProjectSelect
            projects={state.projects.filter(
              (p) => !isClosedProject(p) || p.id === note.projectId
            )}
            value={projectId}
            onChange={setProjectId}
          />
          <Button size="sm" variant="primary" onClick={save} disabled={!trimmedTitle}>
            Save
          </Button>
          <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("view")}>
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="group py-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="font-sans text-[0.85rem] font-medium text-gtc-text">{note.title}</div>
          <p className="font-sans text-[0.8rem] text-gtc-muted">{excerpt(note.body)}</p>
        </div>
        {mode === "view" && (
          // Controls stay in the DOM for keyboard reach and only fade in on
          // hover, so a long notes list is not a wall of buttons.
          <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
            <Button size="sm" variant="ghost" noGlyph onClick={startEdit}>
              Edit
            </Button>
            <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("convert")}>
              Convert
            </Button>
            <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("confirm-delete")}>
              Delete
            </Button>
          </div>
        )}
      </div>
      {mode === "convert" && <ConvertNotePanel note={note} onDone={() => setMode("view")} />}
      {mode === "confirm-delete" && (
        <div className="mt-2 flex items-center gap-2">
          <span className="font-mono text-[0.66rem] lowercase tracking-label text-gtc-muted">
            delete this note?
          </span>
          <Button
            size="sm"
            variant="ghost"
            noGlyph
            onClick={() => dispatch({ type: "DELETE_NOTE", id: note.id })}
          >
            Confirm delete
          </Button>
          <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("view")}>
            Keep
          </Button>
        </div>
      )}
    </div>
  );
}
