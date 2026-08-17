import * as React from "react";
import { Button, Checkbox, Input, SectionLabel, Select, cn } from "@grewelltech/console";
import { ExternalLink, Pencil } from "lucide-react";

import type {
  ActivityLink,
  Project,
  ProjectColor,
  ProjectStatus,
  TaskItem,
} from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { activitiesForProject } from "@/state/selectors";
import { diffDays, formatDay, relativeFromToday, todayISO } from "@/lib/time";
import { MOD_LABEL } from "@/lib/platform";
import { ProjectMark } from "@/components/common/ProjectMark";
import { ActivityTypeIcon } from "@/components/common/activityTypes";
import { ActivityLogStrip, NextActionFlow } from "@/components/common/NextActionFlow";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/Popover";
import { MiniPulse } from "./MiniPulse";
import { NoteRow } from "./NoteRow";
import { DetailsDisclosure } from "@/components/common/DetailsDisclosure";
import { RowActions } from "@/components/common/RowActions";
import { TaskEditor } from "@/components/common/TaskEditor";

const COLOR_RAMP: ProjectColor[] = ["blue", "green", "tan", "violet", "rose", "orange", "steel"];

/** Six statuses with their calm select labels (matches StatusBadge). */
const STATUS_OPTIONS: { value: ProjectStatus; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "waiting", label: "Waiting" },
  { value: "blocked", label: "Blocked" },
  { value: "paused", label: "Paused" },
  { value: "completed", label: "Done" },
  { value: "cancelled", label: "Cancelled" },
];

/** Pulse strip windows; days ending today ("all" derives from history). */
type PulseWindow = "4w" | "8w" | "26w" | "all";
const PULSE_WINDOWS: { id: PulseWindow; label: string; days?: number }[] = [
  { id: "4w", label: "4W", days: 28 },
  { id: "8w", label: "8W", days: 56 },
  { id: "26w", label: "26W", days: 182 },
  { id: "all", label: "All" },
];

/** First sentence of a details blob, for decision summaries. Punctuation
 *  inside numbers ("p99 1.2ms") does not end a sentence. */
function firstSentence(text: string): string {
  const m = text.match(/^[\s\S]*?[.!?](?=\s|$)/);
  return (m ? m[0] : text).trim();
}

/** Quiet borderless mono text button (cancel-grade actions). */
function QuietButton({
  className,
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "rounded-gtc px-1.5 py-1 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted",
        "outline-none transition-colors hover:text-gtc-text focus-visible:shadow-gtc-focus",
        className
      )}
      {...rest}
    >
      {children}
    </button>
  );
}

/** Subtle pencil that appears on hover/focus of its group. */
function HoverPencil({ className }: { className?: string }) {
  return (
    <Pencil
      aria-hidden
      className={cn(
        "h-3 w-3 shrink-0 text-gtc-muted opacity-0 transition-opacity",
        "group-hover:opacity-100 group-focus-visible:opacity-100",
        className
      )}
    />
  );
}

/**
 * Click-to-edit text: quiet display with a hover pencil; clicking swaps to
 * a sans Input (Enter saves) or textarea (Cmd/Ctrl+Enter saves); Escape
 * cancels. Empty values are allowed and show a calm placeholder.
 */
function InlineEditable({
  label,
  value,
  placeholder,
  multiline = false,
  textClassName = "font-sans text-[0.85rem] leading-relaxed text-gtc-text",
  onSave,
}: {
  /** Field name for the accessible edit affordance ("purpose"…). */
  label: string;
  value: string;
  placeholder: string;
  multiline?: boolean;
  textClassName?: string;
  onSave: (value: string) => void;
}) {
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState(value);

  const start = () => {
    setDraft(value);
    setEditing(true);
  };
  const save = () => {
    onSave(draft.trim());
    setEditing(false);
  };
  const cancel = () => setEditing(false);

  if (!editing) {
    return (
      <button
        type="button"
        onClick={start}
        aria-label={`Edit ${label}`}
        className="group flex w-full items-start gap-1.5 rounded-gtc text-left outline-none focus-visible:shadow-gtc-focus"
      >
        <span className={cn(textClassName, !value && "text-gtc-muted")}>
          {value || placeholder}
        </span>
        <HoverPencil className="mt-1" />
      </button>
    );
  }

  const hint = multiline ? `${MOD_LABEL}+ENTER save · ESC cancel` : "ENTER save · ESC cancel";
  return (
    <div>
      {multiline ? (
        <textarea
          autoFocus
          rows={3}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={placeholder}
          aria-label={label}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              cancel();
            }
          }}
          className="w-full resize-y rounded-gtc border border-gtc-line bg-gtc-inset px-2.5 py-2 font-sans text-[0.85rem] leading-relaxed text-gtc-text outline-none transition-shadow placeholder:text-gtc-muted/70 focus:border-gtc-accent focus:shadow-gtc-focus"
        />
      ) : (
        <Input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={placeholder}
          aria-label={label}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              cancel();
            }
          }}
          className="!py-1.5 !font-sans !text-[0.85rem] normal-case"
        />
      )}
      <div className="mt-1.5 flex items-center gap-2">
        <Button size="sm" variant="primary" noGlyph onClick={save}>
          Save
        </Button>
        <QuietButton onClick={cancel}>Cancel</QuietButton>
        <span className="font-mono text-[0.6rem] uppercase tracking-label text-gtc-muted">
          {hint}
        </span>
      </div>
    </div>
  );
}

/** Orientation row: mono label over an editable value. */
function DefRow({
  label,
  value,
  placeholder,
  multiline,
  onSave,
}: {
  label: string;
  value: string;
  placeholder: string;
  multiline?: boolean;
  onSave: (value: string) => void;
}) {
  return (
    <div>
      <dt className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">{label}</dt>
      <dd className="mt-0.5">
        <InlineEditable
          label={label.toLowerCase()}
          value={value}
          placeholder={placeholder}
          multiline={multiline}
          onSave={onSave}
        />
      </dd>
    </div>
  );
}

/** The project mark as an edit affordance: click opens the ramp swatches. */
function ColorEdit({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [open, setOpen] = React.useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Edit project color"
          title="Edit project color"
          className="group relative flex h-6 w-6 items-center justify-center rounded-gtc outline-none transition-colors hover:bg-gtc-tint-accent focus-visible:shadow-gtc-focus"
        >
          <ProjectMark color={project.color} size={12} />
          <Pencil
            aria-hidden
            className="absolute -right-1 -top-1 h-2.5 w-2.5 text-gtc-muted opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
          />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={4} className="p-2">
        <div className="flex items-center gap-1.5" role="group" aria-label="Project color">
          {COLOR_RAMP.map((c) => (
            <button
              key={c}
              type="button"
              aria-label={c}
              aria-pressed={project.color === c}
              onClick={() => {
                dispatch({ type: "UPDATE_PROJECT", id: project.id, patch: { color: c } });
                setOpen(false);
              }}
              className={cn(
                "flex h-5 w-5 items-center justify-center rounded-gtc border outline-none transition-colors",
                "focus-visible:shadow-gtc-focus",
                project.color === c
                  ? "border-gtc-accent"
                  : "border-transparent hover:border-gtc-line"
              )}
            >
              <ProjectMark color={c} size={10} />
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** The project title as a click-to-edit rename. Enter saves, Escape
 *  cancels; a blank name is not a name, so it cancels too rather than
 *  leaving the project with nothing to be called. */
function NameEditable({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState(project.name);
  // Escape unmounts the input while it still has focus, and removing a
  // focused node does dispatch focusout, delegated listeners included.
  // React happens not to deliver that to a deleted fiber's onBlur today,
  // so save() is not reached — but nothing here should depend on that,
  // since the failure would be a silent rename the person cancelled. The
  // ref marks the edit abandoned somewhere a stale closure cannot miss.
  const cancelled = React.useRef(false);

  const start = () => {
    cancelled.current = false;
    setDraft(project.name);
    setEditing(true);
  };
  const cancel = () => {
    cancelled.current = true;
    setEditing(false);
  };
  const save = () => {
    if (cancelled.current) return;
    const name = draft.trim();
    if (name && name !== project.name) {
      dispatch({ type: "UPDATE_PROJECT", id: project.id, patch: { name } });
    }
    setEditing(false);
  };

  return (
    <h2 className="font-sans text-[1.15rem] font-semibold leading-none text-gtc-text">
      {editing ? (
        <Input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          aria-label="Project name"
          placeholder={project.name}
          onBlur={save}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              cancel();
            }
          }}
          className="w-[280px] !py-1 !font-sans !text-[1.15rem] !font-semibold normal-case"
        />
      ) : (
        <button
          type="button"
          onClick={start}
          title="Rename project"
          className="group flex items-center gap-1.5 rounded-gtc text-left outline-none focus-visible:shadow-gtc-focus"
        >
          {project.name}
          {/* The heading takes its accessible name from this button, so the
              name has to lead and the affordance follow — an aria-label
              here would announce the action where the project should be. */}
          <span className="sr-only"> — rename</span>
          <HoverPencil />
        </button>
      )}
    </h2>
  );
}

/** WAITING ON inline field, shown while status is waiting/blocked. */
function WaitingOnInput({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [value, setValue] = React.useState(project.waitingOn ?? "");
  const inputId = `dz-waiting-on-${project.id}`;

  const commit = () => {
    const trimmed = value.trim();
    if (trimmed === (project.waitingOn ?? "")) return;
    dispatch({
      type: "UPDATE_PROJECT",
      id: project.id,
      patch: { waitingOn: trimmed || undefined },
    });
  };

  return (
    <span className="flex items-center gap-1.5">
      <label
        htmlFor={inputId}
        className="whitespace-nowrap font-mono text-[0.66rem] uppercase tracking-label text-gtc-warn"
      >
        waiting on
      </label>
      <Input
        id={inputId}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            setValue(project.waitingOn ?? "");
          }
        }}
        placeholder="Who or what…"
        className="w-[200px] !px-2 !py-1 !font-sans !text-[0.78rem] normal-case"
      />
    </span>
  );
}

/** Tags as chips with click-to-edit into one comma-separated input. */
function TagsEditable({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState("");

  const start = () => {
    setDraft(project.tags.join(", "));
    setEditing(true);
  };
  const save = () => {
    const tags = draft
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
    dispatch({ type: "UPDATE_PROJECT", id: project.id, patch: { tags } });
    setEditing(false);
  };

  if (editing) {
    return (
      <span className="flex items-center gap-1.5">
        <Input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          aria-label="Tags (comma separated)"
          placeholder="tag, another-tag"
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setEditing(false);
            }
          }}
          className="w-[220px] !px-2 !py-1 !text-[0.7rem] lowercase"
        />
      </span>
    );
  }
  return (
    <button
      type="button"
      onClick={start}
      aria-label="Edit tags"
      className="group flex items-center gap-1.5 rounded-gtc outline-none focus-visible:shadow-gtc-focus"
    >
      {project.tags.length === 0 ? (
        <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          Add tags…
        </span>
      ) : (
        project.tags.map((tag) => (
          <span
            key={tag}
            className="rounded-gtc border border-gtc-line px-1.5 py-[2px] font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted"
          >
            {tag}
          </span>
        ))
      )}
      <HoverPencil />
    </button>
  );
}

/** Mono segmented mini-control for the pulse window. */
function PulseWindowControl({
  value,
  onChange,
}: {
  value: PulseWindow;
  onChange: (w: PulseWindow) => void;
}) {
  return (
    <span
      role="group"
      aria-label="Pulse window"
      className="inline-flex overflow-hidden rounded-gtc border border-gtc-line"
    >
      {PULSE_WINDOWS.map((w, i) => (
        <button
          key={w.id}
          type="button"
          aria-pressed={value === w.id}
          onClick={() => onChange(w.id)}
          className={cn(
            "px-2 py-[3px] font-mono text-[0.62rem] uppercase tracking-label outline-none transition-colors",
            "focus-visible:shadow-gtc-focus",
            i > 0 && "border-l border-gtc-line",
            value === w.id
              ? "bg-gtc-tint-accent text-gtc-accent"
              : "text-gtc-muted hover:text-gtc-text"
          )}
        >
          {w.label}
        </button>
      ))}
    </span>
  );
}

function DueChip({ due }: { due: string }) {
  const overdue = due < todayISO();
  return (
    <span
      className={cn(
        "shrink-0 rounded-gtc border px-1.5 py-[1px] font-mono text-[0.62rem] uppercase tracking-label",
        overdue ? "border-gtc-warn-dim text-gtc-warn" : "border-gtc-line text-gtc-muted"
      )}
    >
      due {relativeFromToday(due)}
    </span>
  );
}

/** One task: a single scannable line, its details on demand, and the shared
 *  editor reached the way NoteRow's is — hover or keyboard, quiet until then.
 *
 *  The editor and the action group both moved to components/common for #29,
 *  so a task behaves identically here and in Focus. */
export function TaskRow({ task, onDone }: { task: TaskItem; onDone: () => void }) {
  const [editing, setEditing] = React.useState(false);
  const waiting = task.status === "waiting";

  if (editing) {
    return (
      <div className="py-2">
        <TaskEditor task={task} onDone={() => setEditing(false)} />
      </div>
    );
  }

  return (
    <div className="group relative py-2">
      <div className="flex items-center gap-3">
        {waiting ? (
          <>
            <span className="flex min-w-0 flex-1 items-center gap-2.5">
              <span
                aria-hidden
                className="h-4 w-4 shrink-0 rounded-gtc border border-gtc-line bg-gtc-inset opacity-50"
              />
              <span className="min-w-0 truncate font-sans text-[0.9rem] text-gtc-text">
                {task.title}
              </span>
            </span>
            <span className="shrink-0 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
              waiting on {task.waitingOn ?? "—"}
            </span>
          </>
        ) : (
          <Checkbox
            className="min-w-0 flex-1"
            checked={false}
            onChange={onDone}
            label={<span className="block truncate">{task.title}</span>}
          />
        )}
        <RowActions
          label={`Actions for ${task.title}`}
          actions={[{ label: "Edit", onSelect: () => setEditing(true) }]}
        />
        {task.due && <DueChip due={task.due} />}
      </div>
      <DetailsDisclosure details={task.details} className="ml-6" />
    </div>
  );
}

/** Quiet typed-name delete confirmation at the very bottom of the page. */
function DangerSection({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [confirming, setConfirming] = React.useState(false);
  const [typed, setTyped] = React.useState("");
  const match = typed === project.name;
  const inputId = `dz-delete-confirm-${project.id}`;

  const destroy = () => {
    if (!match) return;
    // Optimistic local cascade (activities/tasks/notes go; captures and
    // reminders detach) + the synced DELETE; land back on the list.
    dispatch({ type: "REMOVE_PROJECT", projectId: project.id });
    dispatch({ type: "CLOSE_PROJECT" });
  };

  return (
    <section className="border-t border-gtc-line pt-5">
      <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        Danger
      </div>
      <p className="mt-2 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Deleting moves this project and its activities, tasks, and notes to the Trash, where
        they can be restored together. Raw captures and reminders keep their link and come back
        with it. Nothing is destroyed until the Trash is emptied or the retention window passes.
      </p>
      {!confirming ? (
        <Button
          variant="danger"
          size="sm"
          className="mt-3"
          onClick={() => {
            setTyped("");
            setConfirming(true);
          }}
        >
          Delete project…
        </Button>
      ) : (
        <div className="mt-3 max-w-[440px] space-y-2 rounded-gtc border border-gtc-danger-dim bg-gtc-inset px-3 py-2.5">
          <label
            htmlFor={inputId}
            className="block font-mono text-[0.62rem] uppercase tracking-label text-gtc-danger"
          >
            Type &ldquo;{project.name}&rdquo; to confirm
          </label>
          <Input
            id={inputId}
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={project.name}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                destroy();
              } else if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                setConfirming(false);
              }
            }}
            className="!py-1.5 !font-sans !text-[0.85rem] normal-case"
          />
          <div className="flex items-center gap-2">
            <Button variant="danger" size="sm" disabled={!match} onClick={destroy}>
              Delete forever
            </Button>
            <Button variant="ghost" size="sm" noGlyph onClick={() => setConfirming(false)}>
              Keep
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

/** Single-project screen: orientation block, pulse, tasks, history. */
export function ProjectDetail({ project }: { project: Project }) {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const acts = activitiesForProject(state, project.id); // date asc
  const actual = acts.filter((a) => !a.planned);
  const recent = [...actual].reverse().slice(0, 10);
  const decisions = [...actual].reverse().filter((a) => a.type === "decision");

  const openTasks = state.tasks
    .filter((t) => t.projectId === project.id && (t.status === "open" || t.status === "waiting"))
    .sort((a, b) => (a.due ?? "9999").localeCompare(b.due ?? "9999") || a.createdAt.localeCompare(b.createdAt));

  const notes = state.notes.filter((n) => n.projectId === project.id);
  const links: ActivityLink[] = [];
  {
    const seen = new Set<string>();
    for (const a of acts) {
      for (const l of a.links) {
        if (seen.has(l.url)) continue;
        seen.add(l.url);
        links.push(l);
      }
    }
  }

  const selectActivity = (id: string) => dispatch({ type: "SELECT_ACTIVITY", id });

  const patch = (p: Partial<Project>) =>
    dispatch({ type: "UPDATE_PROJECT", id: project.id, patch: p });

  const setStatus = (status: ProjectStatus) => {
    const p: Partial<Project> = { status };
    // waitingOn only means something while waiting/blocked — leaving those
    // statuses clears it (server-side too, via the nullable PATCH key).
    if (status !== "waiting" && status !== "blocked" && project.waitingOn) {
      p.waitingOn = undefined;
    }
    patch(p);
  };

  // Session-only pulse window (resets when the detail view unmounts).
  const [pulseWindow, setPulseWindow] = React.useState<PulseWindow>("8w");
  const pulseDays = React.useMemo(() => {
    const preset = PULSE_WINDOWS.find((w) => w.id === pulseWindow)?.days;
    if (preset) return preset;
    // ALL: from the project's first activity, floored at 4 weeks.
    const first = acts[0]?.date;
    return first ? Math.max(28, diffDays(todayISO(), first) + 1) : 28;
  }, [pulseWindow, acts]);

  // Task check-off log prompt: the task completes immediately; the strip
  // only offers to put the finished thing on the timeline too.
  const [taskLog, setTaskLog] = React.useState<{ id: string; title: string } | null>(null);
  const completeTask = (t: TaskItem) => {
    dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "done" } });
    setTaskLog({ id: t.id, title: t.title });
  };

  return (
    <div className="mx-auto max-w-[1000px] space-y-6 px-4 py-6 sm:px-6 lg:px-8">
      {/* Header */}
      <div>
        <Button
          variant="ghost"
          size="sm"
          noGlyph
          onClick={() => dispatch({ type: "CLOSE_PROJECT" })}
          className="-ml-1 gap-1.5"
        >
          <span aria-hidden className="text-[0.85em] leading-none">◂</span>
          Back
        </Button>
        <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2">
          <ColorEdit project={project} />
          <NameEditable key={project.id} project={project} />
          <span className="w-[130px]">
            <Select
              value={project.status}
              onChange={(e) => setStatus(e.target.value as ProjectStatus)}
              aria-label="Project status"
              className="!py-1 !pl-2 !pr-8 !text-[0.68rem] uppercase tracking-label"
            >
              {STATUS_OPTIONS.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </Select>
          </span>
          <TagsEditable project={project} />
          {(project.status === "waiting" || project.status === "blocked") && (
            <WaitingOnInput key={project.id} project={project} />
          )}
        </div>
      </div>

      {/* Orientation block */}
      <div className="grid gap-6 border-b border-gtc-line pb-6 md:grid-cols-2">
        <dl className="space-y-3.5">
          <DefRow
            label="Purpose"
            value={project.purpose}
            placeholder="Add a purpose…"
            multiline
            onSave={(v) => patch({ purpose: v })}
          />
          <DefRow
            label="Desired outcome"
            value={project.outcome}
            placeholder="Add a desired outcome…"
            multiline
            onSave={(v) => patch({ outcome: v })}
          />
          <DefRow
            label="Current focus"
            value={project.currentFocus}
            placeholder="Add a current focus…"
            onSave={(v) => patch({ currentFocus: v })}
          />
        </dl>

        <div className="space-y-4">
          <div className="border-l-2 border-gtc-accent bg-gtc-tint-accent px-3 py-2">
            <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-accent">
              Resume here
            </div>
            <div className="mt-1">
              <InlineEditable
                label="resume context"
                value={project.resumeContext}
                placeholder="Add a resume note for future-you…"
                multiline
                onSave={(v) => patch({ resumeContext: v })}
              />
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between gap-3">
              <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                Next action
              </span>
              <Button
                size="sm"
                onClick={() =>
                  dispatch({
                    type: "SET_QUICK_CAPTURE",
                    open: true,
                    preset: { kind: "activity", projectId: project.id },
                  })
                }
              >
                Log progress
              </Button>
            </div>
            <NextActionFlow project={project} className="mt-1.5" />
          </div>
        </div>
      </div>

      {/* Pulse */}
      <section>
        <SectionLabel
          className="mb-3 mt-0"
          trailing={<PulseWindowControl value={pulseWindow} onChange={setPulseWindow} />}
        >
          Pulse
        </SectionLabel>
        <MiniPulse
          project={project}
          entries={acts}
          showPlanned={state.filters.showPlanned}
          onSelectEntry={selectActivity}
          dayCount={pulseDays}
        />
      </section>

      {/* Open tasks */}
      <section>
        <SectionLabel
          className="mb-2 mt-0"
          trailing={<span className="text-gtc-text">{openTasks.length}</span>}
        >
          Open tasks
        </SectionLabel>
        {taskLog && (
          <div className="mb-2">
            <ActivityLogStrip
              key={taskLog.id}
              project={project}
              initialTitle={taskLog.title}
              skipLabel="Skip"
              onClose={() => setTaskLog(null)}
            />
          </div>
        )}
        {openTasks.length === 0 ? (
          !taskLog && <p className="font-sans text-[0.85rem] text-gtc-muted">No open tasks.</p>
        ) : (
          <div className="divide-y divide-gtc-line">
            {openTasks.map((t) => (
              <TaskRow key={t.id} task={t} onDone={() => completeTask(t)} />
            ))}
          </div>
        )}
      </section>

      {/* Recent activity */}
      <section>
        <SectionLabel
          className="mb-2 mt-0"
          trailing={<span className="text-gtc-text">{recent.length}</span>}
        >
          Recent activity
        </SectionLabel>
        {recent.length === 0 ? (
          <p className="font-sans text-[0.85rem] text-gtc-muted">Nothing logged yet.</p>
        ) : (
          <div className="divide-y divide-gtc-line">
            {recent.map((a) => (
              <button
                key={a.id}
                type="button"
                onClick={() => selectActivity(a.id)}
                className="flex w-full items-center gap-2.5 py-2 text-left outline-none transition-colors hover:bg-gtc-tint-accent focus-visible:shadow-gtc-focus"
              >
                <ActivityTypeIcon type={a.type} className="h-3.5 w-3.5 shrink-0 text-gtc-muted" />
                <span className="min-w-0 flex-1 truncate font-sans text-[0.85rem] text-gtc-text">
                  {a.title}
                </span>
                <span className="shrink-0 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                  {formatDay(a.date)}
                  {a.effortHours ? ` · ${a.effortHours}h` : ""}
                </span>
              </button>
            ))}
          </div>
        )}
      </section>

      {/* Decisions */}
      {decisions.length > 0 && (
        <section>
          <SectionLabel
            className="mb-2 mt-0"
            trailing={<span className="text-gtc-text">{decisions.length}</span>}
          >
            Decisions
          </SectionLabel>
          <div className="divide-y divide-gtc-line">
            {decisions.map((d) => (
              <div key={d.id} className="flex gap-3 py-2">
                <span className="w-14 shrink-0 pt-0.5 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                  {formatDay(d.date)}
                </span>
                <div className="min-w-0">
                  <div className="font-sans text-[0.85rem] font-medium text-gtc-text">
                    {d.title}
                  </div>
                  {d.details && (
                    <p className="font-sans text-[0.8rem] text-gtc-muted">
                      {firstSentence(d.details)}
                    </p>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Notes & links */}
      {(notes.length > 0 || links.length > 0) && (
        <section>
          <SectionLabel className="mb-2 mt-0">Notes &amp; links</SectionLabel>
          <div className={cn("grid gap-6", notes.length > 0 && links.length > 0 && "md:grid-cols-2")}>
            {notes.length > 0 && (
              <div className="divide-y divide-gtc-line">
                {notes.map((n) => (
                  <NoteRow key={n.id} note={n} />
                ))}
              </div>
            )}
            {links.length > 0 && (
              <div className="divide-y divide-gtc-line">
                {links.map((l) => (
                  <a
                    key={l.url}
                    href={l.url}
                    target="_blank"
                    rel="noreferrer"
                    className="flex items-center gap-2 py-1.5 font-sans text-[0.82rem] text-gtc-accent outline-none transition-colors hover:text-gtc-accent-bright focus-visible:shadow-gtc-focus"
                  >
                    <ExternalLink className="h-3.5 w-3.5 shrink-0" aria-hidden />
                    <span className="truncate">{l.label}</span>
                  </a>
                ))}
              </div>
            )}
          </div>
        </section>
      )}

      <DangerSection project={project} />
    </div>
  );
}
