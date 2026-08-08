import { SectionLabel } from "@grewelltech/console";

import type { NoteItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { ProjectSelect } from "@/components/capture/ProjectSelect";
import { NoteRow } from "./NoteRow";

/**
 * Notes that belong to no project.
 *
 * A note with no project is legitimate — the store allows it, MCP's
 * create_note does not require one, and inbox conversion can produce one —
 * but until this existed there was nowhere in the app it rendered. Project
 * detail is the only surface with edit and delete, and it filters strictly
 * by project id, so an unfiled note could be found by search and then not
 * acted on. An LLM could create a note its owner could not fix.
 *
 * Shown only when there are some. A permanently empty "Unfiled" heading on
 * the main projects page would be chrome earning nothing: the section is
 * most discoverable exactly when it has something in it.
 */
export function UnfiledNotes() {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const unfiled = state.notes.filter((n) => !n.projectId);
  if (unfiled.length === 0) return null;

  // Same set the capture rows offer: filing something into a finished
  // project is rarely what is meant, and the list stays short.
  const openProjects = state.projects.filter((p) => !isClosedProject(p));

  const file = (note: NoteItem, projectId: string) => {
    if (!projectId) return;
    dispatch({ type: "UPDATE_NOTE", id: note.id, patch: { projectId } });
  };

  return (
    <section className="mt-8">
      <SectionLabel trailing={<span className="text-gtc-text">{unfiled.length}</span>}>
        Unfiled notes
      </SectionLabel>
      <p className="mb-2 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Notes that belong to no project yet. Give one a project and it moves to
        that project&rsquo;s page.
      </p>
      <ul className="divide-y divide-gtc-line/60">
        {unfiled.map((note) => (
          <li key={note.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 py-1">
            <div className="min-w-0 flex-1">
              <NoteRow note={note} />
            </div>
            {/* basis-full below sm so the select drops under the note rather
                than crushing the title on a phone. */}
            <div className="flex shrink-0 basis-full items-center pt-2 sm:basis-auto sm:pt-3">
              <ProjectSelect
                projects={openProjects}
                value=""
                onChange={(projectId) => file(note, projectId)}
                required
              />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
