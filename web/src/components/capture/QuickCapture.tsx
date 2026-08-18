import * as React from "react";
import { Button, Dialog, Input, cn } from "@grewelltech/console";

import type { ActivityType, ItemKind, ProjectColor, ReminderRepeat } from "@/domain/types";
import { ApiError, api, ensureCatchall, rewriteWithLLM } from "@/api/client";
import { useLLMStatus } from "@/state/useLLMStatus";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { isClosedProject } from "@/state/selectors";
import { useSession } from "@/components/auth/session";
import { newId } from "@/lib/id";
import { nowLocalISO, todayISO, withSeconds } from "@/lib/time";
import { MOD_LABEL } from "@/lib/platform";
import { ProjectMark } from "@/components/common/ProjectMark";
import { Chip, ChipTag } from "./chips";
import { KINDS, KindRow } from "./KindRow";
import { TaskFields } from "./TaskFields";
import { NoteFields } from "./NoteFields";
import { ReminderFields, defaultRemindAt, type WhenChipId } from "./ReminderFields";
import { ActivityFields } from "./ActivityFields";
import { DetailsField } from "./DetailsField";
import { ProjectFields, firstUnusedColor } from "./ProjectFields";

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

/** Placeholder examples per kind; each dialog open rotates to the next
 *  example, and picking a kind swaps to that kind's example. */
const PLACEHOLDERS: Record<ItemKind, string[]> = {
  task: [
    "Need to rotate the runner PATs before Friday.",
    "Fix the flaky session test in loom.",
  ],
  note: [
    "Conntrack RCA — raw notes from the runner-02 incident.",
    "Idea: one-command restore drill for the backup host.",
  ],
  reminder: [
    "Remind me Tuesday morning to email Dan about the RAN550.",
    "Remind me Monday to check the burn-in results.",
  ],
  activity: [
    "Spent 2h debugging the deploy gate flake.",
    "Shipped the session-revocation fix to staging.",
  ],
  project: [
    "Garage workshop lighting refresh.",
    "Homelab observability stack.",
  ],
};

/** True while a Radix menu is open anywhere — Alt+digit then belongs to
 *  the menu layer, not the capture dialog. */
function radixMenuOpen(): boolean {
  return (
    document.querySelector(
      '[data-radix-menu-content][data-state="open"], [role="menu"][data-state="open"]'
    ) !== null
  );
}

/** Cmd/Ctrl+K quick-capture dialog: one line of text in, a task / note /
 *  reminder / activity / project (or a raw inbox item) out. The text is
 *  always the capture; the kind row and its tailored fields only shape
 *  what Create builds — Save to inbox ignores them. */
export function QuickCapture() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const session = useSession();
  const open = state.quickCaptureOpen;

  const [text, setText] = React.useState("");
  const [manualKind, setManualKind] = React.useState<ItemKind | null>(null);
  // Shared across kinds that have a project field (task/note/reminder/activity).
  const [projectId, setProjectId] = React.useState("");
  // Task
  const [taskDue, setTaskDue] = React.useState("");
  // Reminder
  const [whenChip, setWhenChip] = React.useState<WhenChipId | null>("tomorrow");
  const [remindAt, setRemindAt] = React.useState(defaultRemindAt);
  const [repeat, setRepeat] = React.useState<ReminderRepeat | undefined>(undefined);
  // The optional long form, shared by every kind that has one. One piece of
  // state rather than one per kind: switching kind mid-capture keeps what was
  // typed, which is what someone means by changing their mind about what a
  // thing is.
  const [details, setDetails] = React.useState("");
  // Activity
  const [activityType, setActivityType] = React.useState<ActivityType>("work");
  const [activityDate, setActivityDate] = React.useState(todayISO);
  const [activityEffort, setActivityEffort] = React.useState("");
  // Project — null = auto (first unused ramp color for this space).
  const [projectColor, setProjectColor] = React.useState<ProjectColor | null>(null);
  const [projectPurpose, setProjectPurpose] = React.useState("");
  // Transient "captured to <space> inbox" confirmation for cross-space saves.
  const [captureNote, setCaptureNote] = React.useState<string | null>(null);
  const [capturePending, setCapturePending] = React.useState(false);
  // True while create() is mid-flight — guards the one async submit path
  // (unparented activity → ensureCatchall) against a double Enter/click.
  const creatingRef = React.useRef(false);
  // Optional model-backed tidy-up of the typed text. Capture works
  // exactly as before when no model is configured, or when this is
  // simply not used — it is a flourish, never a step.
  const llm = useLLMStatus();
  const [polishing, setPolishing] = React.useState(false);
  // The live capture text, readable from an async callback without closing
  // over a stale copy.
  const textRef = React.useRef(text);
  textRef.current = text;
  // Bumped on every open. A reply that arrives after the dialog was closed
  // and reopened belongs to a capture that is over, and must not land in
  // the new one.
  const captureGeneration = React.useRef(0);
  const inputRef = React.useRef<HTMLInputElement>(null);
  const contentRef = React.useRef<HTMLDivElement>(null);
  const closeTimer = React.useRef<number | null>(null);
  // Bumped per open so placeholder examples rotate between captures.
  const [placeholderTick, setPlaceholderTick] = React.useState(0);

  const liveSpaces = session.spaces.filter((s) => !s.archivedAt);
  const activeIsLive = liveSpaces.some((s) => s.id === session.activeSpaceId);
  // Captures only ever target live spaces (the server refuses archived
  // writes with a 409). An archived active space — archived from another
  // tab, or as the last live one — falls back to the first live space;
  // null means everything is archived and capture is disabled.
  const defaultTargetId = activeIsLive
    ? session.activeSpaceId
    : liveSpaces[0]?.id ?? null;
  const [targetSpaceId, setTargetSpaceId] = React.useState<string | null>(defaultTargetId);

  const crossSpace = targetSpaceId !== null && targetSpaceId !== session.activeSpaceId;
  const noLiveTarget = targetSpaceId === null;

  // Everything except the typed text — the "fresh capture" baseline. The
  // text alone survives a close so an interrupted capture (Escape, or a 401
  // mid-save) keeps its words for the retry.
  const resetMeta = () => {
    setManualKind(null);
    setProjectId("");
    setTaskDue("");
    setDetails("");
    setWhenChip("tomorrow");
    setRemindAt(defaultRemindAt());
    setRepeat(undefined);
    setActivityType("work");
    setActivityDate(todayISO());
    setActivityEffort("");
    setProjectColor(null);
    setProjectPurpose("");
    setCaptureNote(null);
    setCapturePending(false);
    setPolishing(false);
  };

  // Each open is a fresh capture: re-derive the kind defaults (a tab left
  // open overnight must not reuse yesterday's "tomorrow 9am" or activity
  // date), hand kind steering back to auto-suggest, and rotate the
  // placeholder. Then focus the capture input after the Dialog's own panel
  // focus lands.
  React.useEffect(() => {
    if (!open) return;
    captureGeneration.current += 1;
    resetMeta();
    // An opener may preset context ("+ New project" → kind project; "Log
    // progress" inside a project → kind activity + that project). A preset
    // kind is a manual pick, so auto-suggest stops steering this capture.
    const preset = state.quickCapturePreset;
    if (preset?.kind) setManualKind(preset.kind);
    if (preset?.projectId) setProjectId(preset.projectId);
    setPlaceholderTick((t) => t + 1);
    // A dropdown still open under Ctrl+K would float above the modal, own
    // Alt+digits, and eat the first Escape — dismiss it now. The menu's
    // layer preventDefaults the Escape it consumes (and this Dialog defers
    // to consumed Escapes), so the dialog itself stays open.
    if (radixMenuOpen()) {
      document.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })
      );
    }
    const t = window.setTimeout(() => inputRef.current?.focus(), 30);
    // A dismissed menu refocuses its trigger asynchronously just after
    // closing (see useDialogFocusReassert) — re-assert once so the capture
    // input wins without robbing focus the user moved inside the panel.
    const t2 = window.setTimeout(() => {
      const panel = contentRef.current?.closest('[role="dialog"]');
      if (panel && !panel.contains(document.activeElement)) inputRef.current?.focus();
    }, 150);
    return () => {
      window.clearTimeout(t);
      window.clearTimeout(t2);
    };
    // resetMeta only touches setters; deliberately keyed on open alone.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Captures only ever aim at live spaces: start at the default target and
  // re-aim if the active space changes (or is archived from another tab)
  // while the dialog is open.
  React.useEffect(() => {
    if (!open) return;
    setTargetSpaceId(defaultTargetId);
  }, [open, defaultTargetId]);

  React.useEffect(
    () => () => {
      if (closeTimer.current !== null) window.clearTimeout(closeTimer.current);
    },
    []
  );

  const raw = text.trim();
  const suggested = suggestKind(raw);
  const kind = manualKind ?? suggested;
  const showSuggested = manualKind === null && raw.length > 0;

  const openProjects = state.projects.filter((p) => !isClosedProject(p));
  const defaultColor = firstUnusedColor(state.projects);
  const pickedColor = projectColor ?? defaultColor;

  // Leaving a kind resets that kind's specific values; text and the shared
  // project selection carry across (where the field exists).
  const prevKind = React.useRef(kind);
  React.useEffect(() => {
    const prev = prevKind.current;
    if (prev === kind) return;
    prevKind.current = kind;
    switch (prev) {
      case "task":
        setTaskDue("");
        break;
      case "reminder":
        setWhenChip("tomorrow");
        setRemindAt(defaultRemindAt());
        break;
      case "activity":
        setActivityType("work");
        setActivityDate(todayISO());
        setActivityEffort("");
        break;
      case "project":
        setProjectColor(null);
        setProjectPurpose("");
        break;
      case "note":
        break;
    }
  }, [kind]);

  // Alt+1..5 picks the kind from anywhere in the dialog (a manual pick,
  // like a chip click — auto-suggest stops steering). Ignored while a
  // Radix menu is open: those keys belong to the menu layer.
  React.useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey) return;
      const m = /^Digit([1-5])$/.exec(e.code);
      if (!m || radixMenuOpen()) return;
      e.preventDefault();
      setManualKind(KINDS[Number(m[1]) - 1]);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  const close = React.useCallback(
    () => dispatch({ type: "SET_QUICK_CAPTURE", open: false }),
    [dispatch]
  );

  const reset = () => {
    setText("");
    resetMeta();
  };

  // Replaces the typed text with the model's tidied version. Deliberately
  // manual: the fast path stays untouched, and this is here for the moments
  // when the capture is worth a second of care.
  const polish = () => {
    if (!raw || polishing) return;
    const sent = raw;
    const generation = captureGeneration.current;
    setPolishing(true);
    setCaptureNote(null);
    rewriteWithLLM("polish-capture", sent)
      .then((cleaned) => {
        // A different capture is on screen now — this reply is stale.
        if (generation !== captureGeneration.current) return;
        setPolishing(false);
        const next = cleaned.trim();
        if (!next) return;
        // The draft moved on while the model was working: typed into, or
        // saved and cleared. Overwriting it now would throw away words the
        // person wrote after asking for this.
        if (textRef.current.trim() !== sent) {
          setCaptureNote("polish discarded — the text changed while it ran");
          return;
        }
        if (next !== sent) setText(next);
        inputRef.current?.focus();
      })
      .catch((err: unknown) => {
        if (generation !== captureGeneration.current) return;
        const message = err instanceof Error ? err.message : String(err);
        if (err instanceof ApiError && err.status === 401) session.sessionExpired();
        // The typed text is untouched, so this is a dead end, not a loss.
        setCaptureNote(`couldn't polish — ${message}`);
        setPolishing(false);
      });
  };

  // The inbox is deliberately one field, so the long form is folded back into
  // the raw text rather than dropped. splitCapture pulls the two apart again
  // at classify time — in the app and over MCP — so nothing is lost and the
  // capture still costs zero decisions.
  const rawWithDetails = () => (details.trim() === "" ? raw : `${raw}\n\n${details}`);

  const saveToInbox = () => {
    if (!raw || capturePending || targetSpaceId === null) return;
    if (crossSpace) {
      // The capture belongs to another space: post straight to its inbox
      // and leave this store's state untouched.
      const target = liveSpaces.find((s) => s.id === targetSpaceId);
      setCapturePending(true);
      api
        .post(`/api/spaces/${encodeURIComponent(targetSpaceId)}/inbox`, {
          id: newId("inb"),
          raw: rawWithDetails(),
          capturedAt: nowLocalISO(),
          suggestedKind: kind,
          status: "pending",
        })
        .then(() => {
          setCaptureNote(`captured to ${target?.name ?? targetSpaceId} inbox`);
          closeTimer.current = window.setTimeout(() => {
            closeTimer.current = null;
            reset();
            close();
          }, 900);
        })
        .catch((err: unknown) => {
          const message = err instanceof Error ? err.message : String(err);
          console.error("donezo: cross-space capture failed", err);
          // Dead session: the gate overlays sign-in; the typed text stays
          // in the dialog for a retry once re-authenticated.
          if (err instanceof ApiError && err.status === 401) session.sessionExpired();
          setCaptureNote(`capture failed — ${message}`);
          setCapturePending(false);
        });
      return;
    }
    dispatch({
      type: "ADD_INBOX",
      item: {
        id: newId("inb"),
        raw: rawWithDetails(),
        capturedAt: nowLocalISO(),
        suggestedKind: kind,
        suggestedProjectId: projectId || undefined,
        status: "pending",
      },
    });
    reset();
    close();
  };

  const create = async () => {
    // Reentrancy guard: the activity path awaits ensureCatchall, a yield point
    // where a second Enter/click would otherwise dispatch a duplicate activity
    // (the server dedupes the catch-all project, but not the activity row).
    if (!raw || creatingRef.current) return;
    creatingRef.current = true;
    try {
    switch (kind) {
      case "task":
        dispatch({
          type: "ADD_TASK",
          task: {
            id: newId("tsk"),
            projectId: projectId || undefined,
            title: raw,
            details,
            status: "open",
            due: taskDue || undefined,
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
            // With a body of its own, the typed line is a real title rather
            // than the first 60 characters of the body cut mid-word.
            title: details ? raw : raw.slice(0, 60),
            body: details || raw,
            createdAt: todayISO(),
          },
        });
        break;
      case "reminder": {
        if (!remindAt) return;
        dispatch({
          type: "ADD_REMINDER",
          reminder: {
            id: newId("rem"),
            text: raw,
            details,
            remindAt: withSeconds(remindAt),
            projectId: projectId || undefined,
            repeat,
          },
        });
        break;
      }
      case "activity": {
        const hours = Number(activityEffort);
        // No project chosen → file under the space's catch-all. Resolve (and
        // lazily create) it now, before the optimistic add, so the activity
        // points at a real project the timeline can render. INGEST_PROJECT
        // adds the returned catch-all to local state without re-POSTing it.
        let pid = projectId;
        if (!pid) {
          const space = session.activeSpaceId;
          if (!space) return;
          try {
            const catchall = await ensureCatchall(space);
            dispatch({ type: "INGEST_PROJECT", project: catchall });
            pid = catchall.id;
          } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            if (err instanceof ApiError && err.status === 401) session.sessionExpired();
            setCaptureNote(`capture failed — ${message}`);
            return;
          }
        }
        dispatch({
          type: "ADD_ACTIVITY",
          entry: {
            id: newId("act"),
            projectId: pid,
            date: activityDate || todayISO(),
            type: activityType,
            title: raw,
            details,
            effortHours:
              activityEffort.trim() !== "" && Number.isFinite(hours) && hours > 0
                ? hours
                : undefined,
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
        // Append past the current max so a new project lands at the end of the
        // list, matching the server (CreateProject does the same). Setting it
        // here keeps the optimistic order right before the sync round-trips.
        const nextPosition =
          state.projects.reduce((max, p) => Math.max(max, p.position ?? 0), 0) + 1;
        dispatch({
          type: "ADD_PROJECT",
          project: {
            id,
            name: raw.slice(0, 60),
            color: pickedColor,
            purpose: projectPurpose.trim(),
            outcome: "To be defined",
            currentFocus: raw.slice(0, 80),
            nextAction: "Define first concrete step",
            altNextActions: [],
            status: "active",
            resumeContext: "",
            tags: [],
            position: nextPosition,
          },
        });
        break;
      }
    }
    reset();
    close();
    } finally {
      creatingRef.current = false;
    }
  };

  const createDisabled =
    !raw ||
    crossSpace ||
    noLiveTarget ||
    (kind === "reminder" && !remindAt);
  const createTitle = noLiveTarget
    ? "All spaces are archived — unarchive one to capture"
    : crossSpace
      ? "Cross-space capture goes to the inbox — classify it there"
      : kind === "reminder" && !remindAt
        ? "Reminder needs a time"
        : undefined;

  // Enter rules, dialog-wide (document-level so chips, swatches, and the
  // panel itself are covered — after a mouse pick focus rests on a chip):
  //   · Cmd/Ctrl+Enter always saves the raw text to the inbox.
  //   · Plain Enter creates — except on a button whose activation still
  //     means something (an unselected chip selects, footer buttons fire);
  //     re-activating an already-pressed chip is a no-op, so there Enter
  //     falls through to create.
  //   · An IME commit arrives as Enter with isComposing — never a capture.
  // Handlers are read through a ref so one listener spans the whole open.
  const enterCtx = React.useRef({ crossSpace, createDisabled, saveToInbox, create });
  enterCtx.current = { crossSpace, createDisabled, saveToInbox, create };
  React.useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Enter" || e.altKey) return;
      if (e.isComposing || e.keyCode === 229) return;
      const panel = contentRef.current?.closest('[role="dialog"]');
      const target = e.target;
      if (!panel || !(target instanceof HTMLElement) || !panel.contains(target)) return;
      const mod = e.metaKey || e.ctrlKey;
      // A plain Enter inside the multi-line details field is a newline, not a
      // submit. This handler predates the field: when every input on the panel
      // was single-line, capturing every Enter was right. Cmd/Ctrl+Enter still
      // reaches the inbox from inside it, which is the one deliberate submit.
      if (!mod && target.tagName === "TEXTAREA") {
        return;
      }
      if (
        !mod &&
        target.tagName === "BUTTON" &&
        target.getAttribute("aria-pressed") !== "true"
      ) {
        return; // native button activation
      }
      e.preventDefault();
      const ctx = enterCtx.current;
      if (mod || ctx.crossSpace) ctx.saveToInbox();
      else if (!ctx.createDisabled) ctx.create();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  const placeholders = PLACEHOLDERS[kind];
  const placeholder = placeholders[placeholderTick % placeholders.length];

  return (
    <Dialog
      open={open}
      onClose={close}
      title="Quick capture"
      maxWidthClassName="max-w-xl"
      footer={
        <div className="flex w-full items-center gap-3">
          {/* Key hints add nothing on phones (and would crush the buttons).
              Segments wrap as whole units so no chord is ever truncated away. */}
          <span className="hidden min-w-0 flex-1 font-mono text-[0.62rem] leading-[0.85rem] text-gtc-muted sm:block">
            <span className="whitespace-nowrap">ENTER create ·</span>{" "}
            <span className="whitespace-nowrap">{MOD_LABEL}+ENTER inbox ·</span>{" "}
            <span className="whitespace-nowrap">ESC close</span>
          </span>
          <div className="flex-1 sm:hidden" />
          {llm.enabled && (
            <Button
              size="sm"
              variant="ghost"
              noGlyph
              loading={polishing}
              disabled={!raw || capturePending}
              onClick={polish}
              className="whitespace-nowrap"
              title="Tidy up spelling and punctuation without changing what this says"
            >
              Polish
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            noGlyph
            disabled={!raw || capturePending || noLiveTarget}
            onClick={saveToInbox}
            className="whitespace-nowrap"
            title={noLiveTarget ? "All spaces are archived — unarchive one to capture" : undefined}
          >
            Save to inbox
          </Button>
          <Button
            size="sm"
            variant="primary"
            disabled={createDisabled}
            onClick={create}
            className="whitespace-nowrap"
            title={createTitle}
          >
            {/* One flex item so the label keeps a normal word space; the
                kind drops on phone widths where the long labels (CREATE
                REMINDER…) would push past the panel edge. */}
            <span>
              Create<span className="hidden sm:inline"> {kind}</span>
            </span>
          </Button>
        </div>
      }
    >
      <div ref={contentRef} className="space-y-3.5">
        <Input
          ref={inputRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={placeholder}
          aria-label="Capture text"
          className="h-10 !font-sans !text-[0.95rem] normal-case"
        />

        {captureNote && (
          <div
            className="font-mono text-[0.66rem] lowercase tracking-label text-gtc-success"
            role="status"
          >
            {captureNote}
          </div>
        )}

        <KindRow kind={kind} showSuggested={showSuggested} onPick={setManualKind} />

        {/* Tailored fields for the selected kind. All five variants stay
            mounted, stacked in the same grid cell, so the area is always as
            tall as the tallest variant at the current width and text size —
            switching kinds (or aiming at another space) never jumps the
            layout. Inactive variants are visibility-hidden: unfocusable,
            unclickable, and out of the accessibility tree. Cross-space
            captures are inbox-only, so every variant hides (the active
            kind's wrapper carries no visibility of its own and inherits
            the container's hidden state) while the space stays reserved. */}
        <div className={cn("grid", (crossSpace || noLiveTarget) && "invisible")}>
          <div className={cn("col-start-1 row-start-1", kind !== "task" && "invisible")}>
            <TaskFields
              projects={openProjects}
              projectId={projectId}
              onProjectId={setProjectId}
              due={taskDue}
              onDue={setTaskDue}
            />
          </div>
          <div className={cn("col-start-1 row-start-1", kind !== "note" && "invisible")}>
            <NoteFields
              projects={openProjects}
              projectId={projectId}
              onProjectId={setProjectId}
            />
          </div>
          <div className={cn("col-start-1 row-start-1", kind !== "reminder" && "invisible")}>
            <ReminderFields
              projects={openProjects}
              projectId={projectId}
              onProjectId={setProjectId}
              whenChip={whenChip}
              remindAt={remindAt}
              onWhen={(chip, at) => {
                setWhenChip(chip);
                setRemindAt(at);
              }}
              repeat={repeat}
              onRepeat={setRepeat}
            />
          </div>
          <div className={cn("col-start-1 row-start-1", kind !== "activity" && "invisible")}>
            <ActivityFields
              projects={openProjects}
              projectId={projectId}
              onProjectId={setProjectId}
              type={activityType}
              onType={setActivityType}
              date={activityDate}
              onDate={setActivityDate}
              effort={activityEffort}
              onEffort={setActivityEffort}
              catchAllFallback
            />
          </div>
          <div className={cn("col-start-1 row-start-1", kind !== "project" && "invisible")}>
            <ProjectFields
              color={pickedColor}
              onColor={setProjectColor}
              purpose={projectPurpose}
              onPurpose={setProjectPurpose}
            />
          </div>
        </div>

        {/* The long form, for every kind that has one. A project's long form
            is its purpose, which ProjectFields already offers. Kept outside
            the grid above because it is the same control whichever kind is
            selected — switching kind should not throw away what was typed. */}
        {/* Shown for a cross-space capture too: that path is inbox-only, and
            the inbox now carries the long form, so hiding a filled field
            would look exactly like losing it. */}
        {kind !== "project" && !noLiveTarget && (
          <DetailsField
            value={details}
            onChange={setDetails}
            label={kind === "note" ? "Body" : "Details"}
            placeholder={
              kind === "note"
                ? "The note itself; the line above becomes its title."
                : "Context, links, what done looks like — anything too long for one line."
            }
          />
        )}

        {/* Space chips — a non-active space forces save-to-inbox (the
            capture belongs to that space, not this store). */}
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Capture space">
          <span className="mr-1 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
            Space
          </span>
          {liveSpaces.map((s) => (
            <Chip
              key={s.id}
              selected={targetSpaceId === s.id}
              onClick={() => setTargetSpaceId(s.id)}
            >
              <ProjectMark color={s.color} size={6} />
              {s.name}
            </Chip>
          ))}
          {crossSpace && (
            <ChipTag className="lowercase">goes to that space&rsquo;s inbox</ChipTag>
          )}
          {noLiveTarget && (
            <ChipTag className="lowercase">
              all spaces are archived — unarchive one to capture
            </ChipTag>
          )}
        </div>
      </div>
    </Dialog>
  );
}
