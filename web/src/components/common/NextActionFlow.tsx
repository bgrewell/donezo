import * as React from "react";
import { Button, Input, cn } from "@grewelltech/console";

import type { Project, TaskItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { newId } from "@/lib/id";
import { formatDay, todayISO } from "@/lib/time";
import { Chip } from "@/components/capture/chips";
import { ProjectMark } from "@/components/common/ProjectMark";

/** Sans overrides for prose inputs (Input is mono by default). */
const PROSE_INPUT = "!font-sans !text-[0.85rem] normal-case";

/** Quiet borderless text button (Cancel / Decide later / row actions). */
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

/** Open/waiting task in the project whose title matches the action text
 *  (case-insensitive) — finishing the action finishes that task too. */
export function matchingTaskFor(
  tasks: TaskItem[],
  projectId: string,
  actionText: string
): TaskItem | undefined {
  const norm = actionText.trim().toLowerCase();
  if (!norm) return undefined;
  return tasks.find(
    (t) =>
      t.projectId === projectId &&
      (t.status === "open" || t.status === "waiting") &&
      t.title.trim().toLowerCase() === norm
  );
}

/**
 * Inline one-line "log it?" strip: editable title, optional effort, date
 * fixed to today. Shared by the next-action done-flow (primary and
 * alternates) and ProjectDetail's task check-off, so finishing anything
 * offers the same one-keystroke path onto the timeline.
 *
 * Enter logs; Escape cancels when `onCancel` is given, otherwise skips.
 * "Log it" dispatches ADD_ACTIVITY (type work, source manual), then
 * `onClose(true)`; the skip button calls `onClose(false)`.
 */
export function ActivityLogStrip({
  project,
  initialTitle,
  note,
  skipLabel = "Skip logging",
  onClose,
  onCancel,
}: {
  project: Project;
  initialTitle: string;
  /** Extra quiet line, e.g. "also completes task: <title>". */
  note?: string;
  skipLabel?: string;
  /** Called after logging (true) or skipping (false). */
  onClose: (logged: boolean) => void;
  /** When present: a quiet Cancel appears and Escape cancels instead of skipping. */
  onCancel?: () => void;
}) {
  const dispatch = useAppDispatch();
  const [title, setTitle] = React.useState(initialTitle);
  const [effort, setEffort] = React.useState("");
  const today = todayISO();

  const logIt = () => {
    const trimmed = title.trim();
    if (!trimmed) return;
    const hours = Number(effort);
    dispatch({
      type: "ADD_ACTIVITY",
      entry: {
        id: newId("act"),
        projectId: project.id,
        date: today,
        type: "work",
        title: trimmed,
        details: "",
        effortHours:
          effort.trim() !== "" && Number.isFinite(hours) && hours > 0 ? hours : undefined,
        source: "manual",
        tags: [],
        links: [],
      },
    });
    onClose(true);
  };

  return (
    <div
      className="space-y-2 rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-2.5"
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.altKey) {
          e.preventDefault();
          e.stopPropagation();
          logIt();
        } else if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          if (onCancel) onCancel();
          else onClose(false);
        }
      }}
    >
      <div className="flex items-baseline justify-between gap-3">
        <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          Log this?
        </span>
        <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          today · {formatDay(today)}
        </span>
      </div>
      <Input
        autoFocus
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        aria-label="Activity title"
        placeholder="What got done…"
        className={cn("!py-1.5", PROSE_INPUT)}
      />
      {note && (
        <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-success">
          {note}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <label
          htmlFor={`nal-effort-${project.id}`}
          className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted"
        >
          Effort h
        </label>
        <Input
          id={`nal-effort-${project.id}`}
          type="number"
          step={0.5}
          min={0}
          value={effort}
          onChange={(e) => setEffort(e.target.value)}
          placeholder="—"
          className="!w-16 !px-2 !py-1 text-[0.75rem]"
        />
        <div className="flex-1" />
        <Button size="sm" variant="primary" noGlyph disabled={!title.trim()} onClick={logIt}>
          Log it
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={() => onClose(false)}>
          {skipLabel}
        </Button>
        {onCancel && <QuietButton onClick={onCancel}>Cancel</QuietButton>}
      </div>
    </div>
  );
}

/** Inline editor for the primary next-action text. */
function EditRow({
  initial,
  onSave,
  onCancel,
}: {
  initial: string;
  onSave: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = React.useState(initial);
  return (
    <div className="space-y-2">
      <Input
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-label="Next action"
        placeholder="Add a next action…"
        className={cn("!py-1.5", PROSE_INPUT)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onSave(value);
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
      />
      <div className="flex items-center gap-2">
        <Button size="sm" variant="primary" noGlyph onClick={() => onSave(value)}>
          Save
        </Button>
        <QuietButton onClick={onCancel}>Cancel</QuietButton>
      </div>
    </div>
  );
}

/** Done-flow step 2: promote an alternate (or type fresh) to primary. */
function PromoteStrip({
  project,
  onDone,
}: {
  project: Project;
  onDone: () => void;
}) {
  const dispatch = useAppDispatch();
  const alts = project.altNextActions;
  const [value, setValue] = React.useState(alts[0] ?? "");

  const confirm = (chosen: string, remaining: string[]) => {
    const trimmed = chosen.trim();
    if (!trimmed) return;
    // The old primary is done — it never returns to the list.
    dispatch({
      type: "UPDATE_PROJECT",
      id: project.id,
      patch: { nextAction: trimmed, altNextActions: remaining },
    });
    onDone();
  };

  const decideLater = () => {
    dispatch({ type: "UPDATE_PROJECT", id: project.id, patch: { nextAction: "" } });
    onDone();
  };

  return (
    <div
      className="space-y-2 rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-2.5"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          decideLater();
        }
      }}
    >
      <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        What&rsquo;s next?
      </div>
      <Input
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-label="New next action"
        placeholder="Add a next action…"
        className={cn("!py-1.5", PROSE_INPUT)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            // Confirming the editable line consumes the first alternate it
            // was prefilled from; the rest stay listed.
            confirm(value, alts.slice(1));
          }
        }}
      />
      {alts.length > 1 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {alts.slice(1).map((alt, i) => (
            <Chip
              key={`${i + 1}-${alt}`}
              onClick={() => confirm(alt, alts.filter((_, j) => j !== i + 1))}
              className="normal-case tracking-normal"
            >
              {alt}
            </Chip>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="primary"
          noGlyph
          disabled={!value.trim()}
          onClick={() => confirm(value, alts.slice(1))}
        >
          Set next action
        </Button>
        <QuietButton onClick={decideLater}>Decide later</QuietButton>
      </div>
    </div>
  );
}

/** Empty next-action state: calm prompt, inline input, alternate chips. */
function EmptyAction({ project }: { project: Project }) {
  const dispatch = useAppDispatch();
  const [value, setValue] = React.useState("");
  const alts = project.altNextActions;

  const set = (nextAction: string, remaining?: string[]) => {
    const trimmed = nextAction.trim();
    if (!trimmed) return;
    dispatch({
      type: "UPDATE_PROJECT",
      id: project.id,
      patch:
        remaining !== undefined
          ? { nextAction: trimmed, altNextActions: remaining }
          : { nextAction: trimmed },
    });
  };

  return (
    <div className="space-y-2">
      <p className="font-sans text-[0.85rem] leading-relaxed text-gtc-muted">
        No next action — pick one when you&rsquo;re ready.
      </p>
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-label="New next action"
        placeholder="Add a next action…"
        className={cn("!py-1.5", PROSE_INPUT)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            set(value);
          }
        }}
      />
      {alts.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {alts.map((alt, i) => (
            <Chip
              key={`${i}-${alt}`}
              onClick={() => set(alt, alts.filter((_, j) => j !== i))}
              className="normal-case tracking-normal"
            >
              {alt}
            </Chip>
          ))}
        </div>
      )}
    </div>
  );
}

type Flow =
  | { kind: "idle" }
  | { kind: "edit" }
  | { kind: "log"; altIndex?: number }
  | { kind: "promote" };

/**
 * The next-action lifecycle, shared verbatim by ProjectDetail and the
 * Focus view's NextActionPanel:
 *
 *   display → Done → log strip (log/skip) → promotion strip → new action
 *
 * Alternates get quiet "Start instead" (swap with primary) and "Done"
 * (same log strip; completion removes the alternate) row actions. An
 * empty next action renders a calm pick-one state instead of hiding.
 */
export function NextActionFlow({
  project,
  framed = false,
  tourId,
  extraButtons,
  className,
}: {
  project: Project;
  /** Wrap the action area in the Focus panel frame with its accent label. */
  framed?: boolean;
  /** data-tour hook for the framed panel (Focus onboarding). */
  tourId?: string;
  /** Extra buttons after Done/Edit (Open project / Log progress). */
  extraButtons?: React.ReactNode;
  className?: string;
}) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const [flow, setFlow] = React.useState<Flow>({ kind: "idle" });

  // A different project (Focus's "now" thread moving on) resets the flow.
  React.useEffect(() => setFlow({ kind: "idle" }), [project.id]);

  const alts = project.altNextActions;
  const hasAction = project.nextAction.trim() !== "";

  /** The text being completed by the current log step. */
  const doneText = (altIndex?: number) =>
    altIndex === undefined ? project.nextAction : (alts[altIndex] ?? "");

  // After log-or-skip (never cancel): complete the matching task, then
  // promote (primary) or drop the finished alternate (its row).
  const finishDone = (altIndex?: number) => {
    const task = matchingTaskFor(state.tasks, project.id, doneText(altIndex));
    if (task) dispatch({ type: "UPDATE_TASK", id: task.id, patch: { status: "done" } });
    if (altIndex === undefined) {
      setFlow({ kind: "promote" });
    } else {
      dispatch({
        type: "UPDATE_PROJECT",
        id: project.id,
        patch: { altNextActions: alts.filter((_, i) => i !== altIndex) },
      });
      setFlow({ kind: "idle" });
    }
  };

  const saveAction = (value: string) => {
    dispatch({
      type: "UPDATE_PROJECT",
      id: project.id,
      patch: { nextAction: value.trim() },
    });
    setFlow({ kind: "idle" });
  };

  const startInstead = (i: number) => {
    dispatch({
      type: "UPDATE_PROJECT",
      id: project.id,
      patch: {
        nextAction: alts[i],
        altNextActions: alts.map((a, j) => (j === i ? project.nextAction : a)),
      },
    });
  };

  let primary: React.ReactNode;
  switch (flow.kind) {
    case "edit":
      primary = (
        <EditRow
          initial={project.nextAction}
          onSave={saveAction}
          onCancel={() => setFlow({ kind: "idle" })}
        />
      );
      break;
    case "log": {
      const text = doneText(flow.altIndex);
      const task = matchingTaskFor(state.tasks, project.id, text);
      primary = (
        <ActivityLogStrip
          project={project}
          initialTitle={text}
          note={task ? `also completes task: ${task.title}` : undefined}
          onClose={() => finishDone(flow.altIndex)}
          onCancel={() => setFlow({ kind: "idle" })}
        />
      );
      break;
    }
    case "promote":
      primary = <PromoteStrip project={project} onDone={() => setFlow({ kind: "idle" })} />;
      break;
    default:
      primary = !hasAction ? (
        <EmptyAction project={project} />
      ) : (
        <div>
          <p className="max-w-[68ch] font-sans text-[0.95rem] leading-relaxed text-gtc-text">
            {project.nextAction}
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="primary"
              aria-label="Next action done"
              onClick={() => setFlow({ kind: "log" })}
            >
              Done
            </Button>
            <Button
              size="sm"
              variant="ghost"
              noGlyph
              aria-label="Edit next action"
              onClick={() => setFlow({ kind: "edit" })}
            >
              Edit
            </Button>
            {extraButtons}
          </div>
        </div>
      );
  }

  // Alternates list — hidden while promoting (the strip already offers them).
  const altRows =
    flow.kind !== "promote" && hasAction && alts.length > 0 ? (
      <div className="mt-2.5 space-y-1 px-1">
        <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">Or</div>
        {alts.map((alt, i) => (
          <div key={`${i}-${alt}`} className="group flex min-w-0 items-center gap-2">
            <ProjectMark color={project.color} size={6} muted />
            <span className="min-w-0 flex-1 font-sans text-[0.8rem] text-gtc-muted">{alt}</span>
            <QuietButton
              aria-label={`Start instead: ${alt}`}
              onClick={() => startInstead(i)}
              className="shrink-0 opacity-0 focus-visible:opacity-100 group-focus-within:opacity-100 group-hover:opacity-100"
            >
              Start instead
            </QuietButton>
            <QuietButton
              aria-label={`Done: ${alt}`}
              onClick={() => setFlow({ kind: "log", altIndex: i })}
              className="shrink-0 opacity-0 focus-visible:opacity-100 group-focus-within:opacity-100 group-hover:opacity-100"
            >
              Done
            </QuietButton>
          </div>
        ))}
      </div>
    ) : null;

  if (framed) {
    return (
      <div className={className}>
        <div
          data-tour={tourId}
          className="rounded-gtc border border-gtc-line bg-gtc-panel bg-gtc-sheen px-4 py-3"
        >
          <div className="font-mono text-[0.64rem] uppercase tracking-label text-gtc-accent">
            Next action
          </div>
          <div className="mt-1.5">{primary}</div>
        </div>
        {altRows}
      </div>
    );
  }
  return (
    <div className={className}>
      {primary}
      {altRows}
    </div>
  );
}
