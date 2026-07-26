import * as React from "react";
import { Button, Dialog, Input, Select, cn } from "@grewelltech/console";

import type { ItemKind, Project, ProjectColor } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { latestActivityDate } from "@/state/selectors";
import { newId } from "@/lib/id";
import { addDaysISO, nowLocalISO, todayISO } from "@/lib/time";
import { MOD_LABEL } from "@/lib/platform";
import { ProjectMark } from "@/components/common/ProjectMark";

const KINDS: ItemKind[] = ["task", "note", "reminder", "activity", "project"];

const COLOR_RAMP: ProjectColor[] = ["blue", "green", "tan", "violet", "rose", "orange", "steel"];

/** Heuristic kind suggestion for raw captured text. */
function suggestKind(text: string): ItemKind {
  if (/remind/i.test(text)) return "reminder";
  if (/^(spent|fixed|deployed|migrated|shipped|debugged)/i.test(text) || /\b(spent|took) \d/.test(text)) {
    return "activity";
  }
  if (/\b(bug|todo|need to|should)\b/i.test(text)) return "task";
  return "task";
}

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32)
    .replace(/-+$/, "");
}

/** Cmd/Ctrl+K quick-capture dialog: one line of text in, a task / note /
 *  reminder / activity / project (or a raw inbox item) out. */
export function QuickCapture() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const open = state.quickCaptureOpen;

  const [text, setText] = React.useState("");
  const [manualKind, setManualKind] = React.useState<ItemKind | null>(null);
  const [projectId, setProjectId] = React.useState("");
  const inputRef = React.useRef<HTMLInputElement>(null);

  // Focus the capture input after the Dialog's own panel focus lands.
  React.useEffect(() => {
    if (!open) return;
    const t = window.setTimeout(() => inputRef.current?.focus(), 30);
    return () => window.clearTimeout(t);
  }, [open]);

  const raw = text.trim();
  const suggested = suggestKind(raw);
  const kind = manualKind ?? suggested;
  const showSuggested = manualKind === null && raw.length > 0;

  const openProjects = state.projects.filter((p) => p.status !== "completed");
  const recentProjects: Project[] = [...openProjects]
    .map((p) => ({ p, latest: latestActivityDate(state, p.id) ?? "" }))
    .sort((a, b) => b.latest.localeCompare(a.latest))
    .slice(0, 3)
    .map((x) => x.p);

  const close = React.useCallback(
    () => dispatch({ type: "SET_QUICK_CAPTURE", open: false }),
    [dispatch]
  );

  const reset = () => {
    setText("");
    setManualKind(null);
    setProjectId("");
  };

  const saveToInbox = () => {
    if (!raw) return;
    dispatch({
      type: "ADD_INBOX",
      item: {
        id: newId("inb"),
        raw,
        capturedAt: nowLocalISO(),
        suggestedKind: kind,
        suggestedProjectId: projectId || undefined,
        status: "pending",
      },
    });
    reset();
    close();
  };

  const create = () => {
    if (!raw) return;
    switch (kind) {
      case "task":
        dispatch({
          type: "ADD_TASK",
          task: {
            id: newId("tsk"),
            projectId: projectId || undefined,
            title: raw,
            status: "open",
            createdAt: todayISO(),
          },
        });
        break;
      case "note":
        dispatch({
          type: "ADD_NOTE",
          note: {
            id: newId("note"),
            projectId: projectId || undefined,
            title: raw.slice(0, 60),
            body: raw,
            createdAt: todayISO(),
          },
        });
        break;
      case "reminder":
        dispatch({
          type: "ADD_REMINDER",
          reminder: {
            id: newId("rem"),
            text: raw,
            remindAt: `${addDaysISO(todayISO(), 1)}T09:00:00`,
            projectId: projectId || undefined,
          },
        });
        break;
      case "activity": {
        if (!projectId) return;
        dispatch({
          type: "ADD_ACTIVITY",
          entry: {
            id: newId("act"),
            projectId,
            date: todayISO(),
            type: "work",
            title: raw,
            details: "",
            source: "capture",
            tags: [],
            links: [],
          },
        });
        break;
      }
      case "project": {
        const slug = slugify(raw);
        const id = !slug || state.projects.some((p) => p.id === slug) ? newId(slug || "proj") : slug;
        const used = new Set(state.projects.map((p) => p.color));
        const color =
          COLOR_RAMP.find((c) => !used.has(c)) ?? COLOR_RAMP[state.projects.length % COLOR_RAMP.length];
        dispatch({
          type: "ADD_PROJECT",
          project: {
            id,
            name: raw.slice(0, 60),
            color,
            purpose: raw,
            outcome: "To be defined",
            currentFocus: raw.slice(0, 80),
            nextAction: "Define first concrete step",
            altNextActions: [],
            status: "active",
            resumeContext: "",
            tags: [],
          },
        });
        break;
      }
    }
    reset();
    close();
  };

  const createDisabled = !raw || (kind === "activity" && !projectId);

  return (
    <Dialog
      open={open}
      onClose={close}
      title="Quick capture"
      maxWidthClassName="max-w-xl"
      footer={
        <div className="flex w-full items-center gap-3">
          {/* Key hints add nothing on phones (and would crush the buttons). */}
          <span className="hidden min-w-0 flex-1 truncate whitespace-nowrap font-mono text-[0.62rem] text-gtc-muted sm:block">
            ENTER create · {MOD_LABEL}+ENTER inbox · ESC close
          </span>
          <div className="flex-1 sm:hidden" />
          <Button
            size="sm"
            variant="ghost"
            noGlyph
            disabled={!raw}
            onClick={saveToInbox}
            className="whitespace-nowrap"
          >
            Save to inbox
          </Button>
          <Button
            size="sm"
            variant="primary"
            disabled={createDisabled}
            onClick={create}
            className="whitespace-nowrap"
            title={kind === "activity" && !projectId ? "Activity needs a project" : undefined}
          >
            Create {kind}
          </Button>
        </div>
      }
    >
      <div className="space-y-3.5">
        <Input
          ref={inputRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter") return;
            e.preventDefault();
            if (e.metaKey || e.ctrlKey) saveToInbox();
            else if (!createDisabled) create();
          }}
          placeholder="Remind me Tuesday morning to email Dan about the RAN550."
          aria-label="Capture text"
          className="h-10 !font-sans !text-[0.95rem] normal-case"
        />

        {/* Kind chips */}
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Item kind">
          {KINDS.map((k) => {
            const selected = kind === k;
            return (
              <button
                key={k}
                type="button"
                aria-pressed={selected}
                onClick={() => setManualKind(k)}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-gtc border px-2 py-1",
                  "font-mono text-[0.64rem] uppercase tracking-chrome outline-none transition-colors",
                  "focus-visible:shadow-gtc-focus",
                  selected
                    ? "border-gtc-accent bg-gtc-tint-accent text-gtc-accent"
                    : "border-gtc-line text-gtc-muted hover:text-gtc-text"
                )}
              >
                {k}
                {selected && showSuggested && (
                  <span className="font-mono text-[0.56rem] lowercase tracking-normal text-gtc-muted">
                    suggested
                  </span>
                )}
              </button>
            );
          })}
        </div>

        {/* Project row */}
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
            Project
          </span>
          {recentProjects.map((p) => {
            const selected = projectId === p.id;
            return (
              <button
                key={p.id}
                type="button"
                aria-pressed={selected}
                onClick={() => setProjectId(selected ? "" : p.id)}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-gtc border px-2 py-1",
                  "font-mono text-[0.64rem] uppercase tracking-chrome outline-none transition-colors",
                  "focus-visible:shadow-gtc-focus",
                  selected
                    ? "border-gtc-accent bg-gtc-tint-accent text-gtc-accent"
                    : "border-gtc-line text-gtc-muted hover:text-gtc-text"
                )}
              >
                <ProjectMark color={p.color} size={6} />
                {p.name}
              </button>
            );
          })}
          <div className="basis-full">
            <Select
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              aria-label="Project"
              className="!py-1.5 !text-[0.75rem]"
            >
              <option value="">No project</option>
              {openProjects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </div>
        </div>
      </div>
    </Dialog>
  );
}
