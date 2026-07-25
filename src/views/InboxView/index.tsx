import * as React from "react";
import { ChevronDown } from "lucide-react";
import { Button, Input, SectionLabel, Select, cn } from "@grewelltech/aether";
import { nextMonday } from "date-fns";

import type { InboxItem, ItemKind, Project, ProjectColor } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { projectById } from "@/state/selectors";
import { newId } from "@/lib/id";
import { addDaysISO, relativeFromToday, toISODate, todayISO } from "@/lib/time";
import { EmptyState } from "@/components/common/EmptyState";
import { ProjectMark } from "@/components/common/ProjectMark";

/** Softer hairline than border-ae-line (ae tokens don't support /alpha). */
const HAIRLINE_SOFT = "border-[color-mix(in_srgb,var(--ae-border)_60%,transparent)]";

const KINDS: ItemKind[] = ["task", "note", "reminder", "activity", "project"];

const REMIND_CHOICES = [
  { id: "tomorrow", label: "Tomorrow 9am" },
  { id: "monday", label: "Monday 9am" },
  { id: "nextweek", label: "Next week" },
] as const;

type RemindChoice = (typeof REMIND_CHOICES)[number]["id"];

/** Resolve a quick-time chip to an ISO datetime. */
function remindAtFor(choice: RemindChoice): string {
  switch (choice) {
    case "tomorrow":
      return `${addDaysISO(todayISO(), 1)}T09:00:00`;
    case "monday":
      return `${toISODate(nextMonday(new Date()))}T09:00:00`;
    case "nextweek":
      return `${addDaysISO(todayISO(), 7)}T09:00:00`;
  }
}

const COLOR_RAMP: ProjectColor[] = ["blue", "green", "tan", "violet", "rose", "orange", "steel"];

/** First unused ramp color, cycling once the ramp is exhausted. */
function nextRampColor(projects: Project[]): ProjectColor {
  const used = new Set(projects.map((p) => p.color));
  return COLOR_RAMP.find((c) => !used.has(c)) ?? COLOR_RAMP[projects.length % COLOR_RAMP.length];
}

/** Mono selection chip — same pattern as quick capture. */
function Chip({
  selected,
  onClick,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={cn(
        "rounded-ae border px-2 py-1 font-mono text-[0.62rem] uppercase tracking-chrome transition-colors",
        "focus-visible:outline-none focus-visible:shadow-ae-focus",
        selected
          ? "border-ae-accent bg-ae-tint-accent text-ae-accent"
          : "border-ae-line text-ae-muted hover:border-ae-accent-dim hover:text-ae-text"
      )}
    >
      {children}
    </button>
  );
}

/** Compact mono field label for the classification area. */
function FieldLabel({ htmlFor, children }: { htmlFor?: string; children: React.ReactNode }) {
  const cls = "mb-1.5 block font-mono text-[0.62rem] uppercase tracking-label text-ae-muted";
  if (htmlFor) {
    return (
      <label htmlFor={htmlFor} className={cls}>
        {children}
      </label>
    );
  }
  return <div className={cls}>{children}</div>;
}

/** Inline classification controls for one pending capture. */
function ClassifyPanel({ item, onCollapse }: { item: InboxItem; onCollapse: () => void }) {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const [kind, setKind] = React.useState<ItemKind>(item.suggestedKind);
  const [projectId, setProjectId] = React.useState(item.suggestedProjectId ?? "");
  const [remindChoice, setRemindChoice] = React.useState<RemindChoice>("tomorrow");
  const [due, setDue] = React.useState("");

  const openProjects = state.projects.filter((p) => p.status !== "completed");
  const needsProject = kind === "activity" && !projectId;
  const selectId = `inbox-project-${item.id}`;
  const dueId = `inbox-due-${item.id}`;

  const convert = () => {
    const raw = item.raw.trim();
    const pid = projectId || undefined;
    const base = { type: "CONVERT_INBOX" as const, id: item.id, kind };
    switch (kind) {
      case "task":
        dispatch({
          ...base,
          task: {
            id: newId("task"),
            projectId: pid,
            title: raw,
            status: "open",
            due: due || undefined,
            createdAt: todayISO(),
          },
        });
        break;
      case "note":
        dispatch({
          ...base,
          note: {
            id: newId("note"),
            projectId: pid,
            title: raw.slice(0, 60),
            body: raw,
            createdAt: todayISO(),
          },
        });
        break;
      case "reminder":
        dispatch({
          ...base,
          reminder: {
            id: newId("rem"),
            text: raw,
            remindAt: remindAtFor(remindChoice),
            projectId: pid,
          },
        });
        break;
      case "activity":
        if (!pid) return;
        dispatch({
          ...base,
          activity: {
            id: newId("act"),
            projectId: pid,
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
      case "project":
        dispatch({
          ...base,
          project: {
            id: newId("proj"),
            name: raw.slice(0, 60),
            color: nextRampColor(state.projects),
            purpose: "",
            outcome: "",
            currentFocus: "",
            nextAction: "",
            altNextActions: [],
            status: "active",
            resumeContext: "",
            tags: [],
          },
        });
        break;
    }
    onCollapse();
  };

  const dismiss = () => {
    dispatch({ type: "UPDATE_INBOX", id: item.id, patch: { status: "dismissed" } });
    onCollapse();
  };

  return (
    <div className="mb-3 space-y-3 rounded-ae border border-ae-line bg-ae-inset px-3 py-3">
      <div role="group" aria-label="File as">
        <FieldLabel>File as</FieldLabel>
        <div className="flex flex-wrap gap-1.5">
          {KINDS.map((k) => (
            <Chip key={k} selected={kind === k} onClick={() => setKind(k)}>
              {k}
            </Chip>
          ))}
        </div>
      </div>

      {kind !== "project" && (
        <div className="flex flex-wrap items-start gap-x-5 gap-y-3">
          <div className="w-full max-w-[240px]">
            <FieldLabel htmlFor={selectId}>Project</FieldLabel>
            <Select
              id={selectId}
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="py-1.5 text-[0.75rem]"
            >
              <option value="">No project</option>
              {openProjects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </div>

          {kind === "reminder" && (
            <div role="group" aria-label="Remind at">
              <FieldLabel>Remind</FieldLabel>
              <div className="flex flex-wrap gap-1.5">
                {REMIND_CHOICES.map((c) => (
                  <Chip
                    key={c.id}
                    selected={remindChoice === c.id}
                    onClick={() => setRemindChoice(c.id)}
                  >
                    {c.label}
                  </Chip>
                ))}
              </div>
            </div>
          )}

          {kind === "task" && (
            <div className="w-[170px]">
              <FieldLabel htmlFor={dueId}>Due — optional</FieldLabel>
              <Input
                id={dueId}
                type="date"
                value={due}
                onChange={(e) => setDue(e.target.value)}
                className="py-1.5 text-[0.75rem]"
              />
            </div>
          )}
        </div>
      )}

      <div className="flex items-center gap-2 pt-0.5">
        <Button variant="primary" size="sm" onClick={convert} disabled={needsProject}>
          Convert
        </Button>
        <Button size="sm" onClick={dismiss}>
          Dismiss
        </Button>
        <Button size="sm" onClick={onCollapse}>
          Later
        </Button>
        {needsProject && (
          <span className="ml-1 font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
            Needs a project
          </span>
        )}
      </div>
    </div>
  );
}

/** One pending capture: collapsed row + expandable classification area. */
function PendingRow({
  item,
  expanded,
  onToggle,
  onCollapse,
}: {
  item: InboxItem;
  expanded: boolean;
  onToggle: () => void;
  onCollapse: () => void;
}) {
  const state = useAppState();
  const suggested = projectById(state, item.suggestedProjectId);
  const panelId = `inbox-panel-${item.id}`;

  return (
    <li className={cn("border-b", HAIRLINE_SOFT)}>
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={expanded ? panelId : undefined}
        onClick={onToggle}
        className={cn(
          "group -mx-2 flex w-[calc(100%+1rem)] items-start gap-3 rounded-ae px-2 py-3 text-left",
          "transition-colors hover:bg-ae-tint-accent focus-visible:outline-none focus-visible:shadow-ae-focus"
        )}
      >
        <span className="min-w-0 flex-1">
          <span className="block text-[0.9rem] leading-snug text-ae-text">{item.raw}</span>
          <span className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
            <span>{relativeFromToday(item.capturedAt.slice(0, 10))}</span>
            <span aria-hidden>·</span>
            <span>suggested: {item.suggestedKind}</span>
            {suggested && (
              <>
                <span aria-hidden>·</span>
                <span className="inline-flex items-center gap-1.5">
                  <ProjectMark color={suggested.color} size={6} />
                  {suggested.name}
                </span>
              </>
            )}
          </span>
        </span>
        <ChevronDown
          aria-hidden
          className={cn(
            "mt-1 h-3.5 w-3.5 shrink-0 text-ae-muted transition-transform group-hover:text-ae-text",
            expanded && "rotate-180"
          )}
        />
      </button>
      {expanded && (
        <div id={panelId} className="mt-2">
          <ClassifyPanel item={item} onCollapse={onCollapse} />
        </div>
      )}
    </li>
  );
}

/** One converted/dismissed capture, single muted line. */
function HandledRow({ item }: { item: InboxItem }) {
  return (
    <li className={cn("flex items-baseline gap-3 border-b py-2", HAIRLINE_SOFT)}>
      <span className="min-w-0 flex-1 truncate text-[0.8rem] text-ae-muted">{item.raw}</span>
      <span
        className={cn(
          "w-20 shrink-0 text-right font-mono text-[0.62rem] uppercase tracking-label",
          item.status === "converted" ? "text-ae-success" : "text-ae-muted"
        )}
      >
        {item.status}
      </span>
      <span className="w-20 shrink-0 text-right font-mono text-[0.62rem] uppercase tracking-label text-ae-muted">
        {relativeFromToday(item.capturedAt.slice(0, 10))}
      </span>
    </li>
  );
}

/** Inbox: raw captures become structured items when the user is ready. */
export default function InboxView() {
  const state = useAppState();
  const [expandedId, setExpandedId] = React.useState<string | null>(null);
  const [showHandled, setShowHandled] = React.useState(false);

  const pending = state.inbox
    .filter((i) => i.status === "pending")
    .sort((a, b) => b.capturedAt.localeCompare(a.capturedAt));
  const handled = state.inbox
    .filter((i) => i.status !== "pending")
    .sort((a, b) => b.capturedAt.localeCompare(a.capturedAt));

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[820px] px-8 py-6">
        <SectionLabel trailing={<span className="text-ae-text">{pending.length}</span>}>
          Inbox
        </SectionLabel>
        <p className="mb-4 text-[0.85rem] text-ae-muted">
          Captured thoughts, unfiled. Classify them when it&rsquo;s cheap — or don&rsquo;t.
        </p>

        {pending.length === 0 ? (
          <EmptyState title="Inbox clear" hint="New captures land here from Cmd+K." />
        ) : (
          <ul>
            {pending.map((item) => (
              <PendingRow
                key={item.id}
                item={item}
                expanded={expandedId === item.id}
                onToggle={() => setExpandedId(expandedId === item.id ? null : item.id)}
                onCollapse={() => setExpandedId(null)}
              />
            ))}
          </ul>
        )}

        {handled.length > 0 && (
          <div className="mt-10">
            <div className="flex items-center gap-3">
              <SectionLabel
                className="flex-1"
                trailing={<span className="text-ae-text">{handled.length}</span>}
              >
                Handled
              </SectionLabel>
              <Button
                size="sm"
                onClick={() => setShowHandled((v) => !v)}
                aria-expanded={showHandled}
              >
                {showHandled ? "Hide" : "Show"}
              </Button>
            </div>
            {showHandled && (
              <ul>
                {handled.map((item) => (
                  <HandledRow key={item.id} item={item} />
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
