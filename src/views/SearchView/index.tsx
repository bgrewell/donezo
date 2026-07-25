import * as React from "react";
import { Bell } from "lucide-react";
import { Button, Input, SectionLabel, cn } from "@grewelltech/console";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { projectById } from "@/state/selectors";
import { formatDay } from "@/lib/time";
import { EmptyState } from "@/components/common/EmptyState";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";
import { ActivityTypeIcon } from "@/components/common/activityTypes";

const META = "font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted";
const ROW_BTN =
  "-mx-2 flex w-full items-center gap-3 px-2 py-2 text-left transition-colors " +
  "hover:bg-gtc-tint-accent focus-visible:outline-none focus-visible:shadow-gtc-focus";
const GROUP_CAP = 8;

/** Locally styled match highlight. */
function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim().toLowerCase();
  if (!q) return <>{text}</>;
  const lower = text.toLowerCase();
  const out: React.ReactNode[] = [];
  let pos = 0;
  for (let idx = lower.indexOf(q); idx !== -1; idx = lower.indexOf(q, pos)) {
    if (idx > pos) out.push(text.slice(pos, idx));
    out.push(
      <mark
        key={idx}
        className="rounded-gtc bg-gtc-tint-accent-strong px-0.5 text-gtc-accent-bright"
      >
        {text.slice(idx, idx + q.length)}
      </mark>
    );
    pos = idx + q.length;
  }
  if (pos < text.length) out.push(text.slice(pos));
  return <>{out}</>;
}

/** Short window of text around the first match, with ellipses. */
function snippet(text: string, q: string, span = 100): string {
  const clean = text.replace(/\s+/g, " ").trim();
  const idx = clean.toLowerCase().indexOf(q);
  if (idx === -1) return clean.length > span ? `${clean.slice(0, span)}…` : clean;
  const start = Math.max(0, idx - Math.floor((span - q.length) / 2));
  const end = Math.min(clean.length, start + span);
  return `${start > 0 ? "…" : ""}${clean.slice(start, end)}${end < clean.length ? "…" : ""}`;
}

/** Result group: SectionLabel + capped rows + "+N more" expander. */
function Group({
  label,
  count,
  expanded,
  onExpand,
  children,
}: {
  label: string;
  count: number;
  expanded: boolean;
  onExpand: () => void;
  children: React.ReactNode;
}) {
  if (count === 0) return null;
  return (
    <section>
      <SectionLabel className="my-0 mb-1" trailing={<span className="text-gtc-text">{count}</span>}>
        {label}
      </SectionLabel>
      <div>{children}</div>
      {count > GROUP_CAP && !expanded && (
        <Button size="sm" noGlyph className="mt-1.5 px-2 py-1 text-[0.64rem]" onClick={onExpand}>
          +{count - GROUP_CAP} more
        </Button>
      )}
    </section>
  );
}

/** Search across projects, activity, tasks, notes, reminders, and inbox. */
export default function SearchView() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const query = state.searchQuery;
  const q = query.trim().toLowerCase();

  const [expandedGroups, setExpandedGroups] = React.useState<Record<string, boolean>>({});
  const [openItems, setOpenItems] = React.useState<Record<string, boolean>>({});

  // A new query starts a fresh result set: collapse expanders and details.
  React.useEffect(() => {
    setExpandedGroups({});
    setOpenItems({});
  }, [q]);

  const results = React.useMemo(() => {
    if (!q) return null;
    const has = (s: string | undefined) => !!s && s.toLowerCase().includes(q);
    return {
      projects: state.projects.filter(
        (p) => has(p.name) || has(p.purpose) || has(p.currentFocus) || p.tags.some(has)
      ),
      activities: state.activities
        .filter((a) => has(a.title) || has(a.details) || a.tags.some(has))
        .sort((a, b) => b.date.localeCompare(a.date)),
      tasks: state.tasks.filter((t) => has(t.title) || has(t.waitingOn)),
      notes: state.notes.filter((n) => has(n.title) || has(n.body)),
      reminders: state.reminders.filter((r) => has(r.text)),
      inbox: state.inbox.filter((i) => has(i.raw)),
    };
  }, [q, state.projects, state.activities, state.tasks, state.notes, state.reminders, state.inbox]);

  const groupCounts = results
    ? ([
        ["projects", results.projects.length],
        ["activity", results.activities.length],
        ["tasks", results.tasks.length],
        ["notes", results.notes.length],
        ["reminders", results.reminders.length],
        ["inbox", results.inbox.length],
      ] as const)
    : [];
  const total = groupCounts.reduce((sum, [, n]) => sum + n, 0);

  const cap = <T,>(items: T[], group: string) =>
    expandedGroups[group] ? items : items.slice(0, GROUP_CAP);
  const expand = (group: string) => () =>
    setExpandedGroups((s) => ({ ...s, [group]: true }));
  const toggleItem = (id: string) => () =>
    setOpenItems((s) => ({ ...s, [id]: !s[id] }));

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[880px] space-y-6 px-8 py-6">
        <div>
          <Input
            autoFocus
            value={query}
            onChange={(e) => dispatch({ type: "SET_SEARCH_QUERY", query: e.target.value })}
            placeholder="Search projects, activity, tasks, notes…"
            aria-label="Search everything"
            className="h-9 py-0 !font-sans text-[0.9rem]"
          />
          {q && (
            <div className={cn(META, "mt-2")}>
              {total} {total === 1 ? "result" : "results"}
              {groupCounts
                .filter(([, n]) => n > 0)
                .map(([label, n]) => ` · ${n} ${label}`)
                .join("")}
            </div>
          )}
        </div>

        {!q && (
          <EmptyState
            title="Search everything"
            hint="Matches project names, tags, activity, tasks, notes, reminders, and raw captures."
          />
        )}

        {q && results && total === 0 && (
          <EmptyState
            title="No matches"
            hint="Try a project name, tag, or a word from an entry."
          />
        )}

        {q && results && total > 0 && (
          <div className="space-y-6">
            <Group
              label="Projects"
              count={results.projects.length}
              expanded={!!expandedGroups.projects}
              onExpand={expand("projects")}
            >
              {cap(results.projects, "projects").map((p) => (
                <div key={p.id} className="border-b border-gtc-line/60">
                  <button
                    type="button"
                    className={ROW_BTN}
                    onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: p.id })}
                  >
                    <ProjectMark color={p.color} />
                    <span className="min-w-0 flex-1 truncate text-[0.85rem] text-gtc-text">
                      <Highlight text={p.name} query={q} />
                    </span>
                    <StatusBadge status={p.status} />
                  </button>
                </div>
              ))}
            </Group>

            <Group
              label="Activity"
              count={results.activities.length}
              expanded={!!expandedGroups.activity}
              onExpand={expand("activity")}
            >
              {cap(results.activities, "activity").map((a) => {
                const proj = projectById(state, a.projectId);
                return (
                  <div key={a.id} className="border-b border-gtc-line/60">
                    <button
                      type="button"
                      className={ROW_BTN}
                      onClick={() => dispatch({ type: "SELECT_ACTIVITY", id: a.id })}
                    >
                      <ActivityTypeIcon type={a.type} className="h-3.5 w-3.5 shrink-0 text-gtc-muted" />
                      <span className="min-w-0 flex-1 truncate text-[0.85rem] text-gtc-text">
                        <Highlight text={a.title} query={q} />
                      </span>
                      <span className={cn(META, "shrink-0")}>{formatDay(a.date)}</span>
                      {proj && <ProjectMark color={proj.color} />}
                    </button>
                  </div>
                );
              })}
            </Group>

            <Group
              label="Tasks"
              count={results.tasks.length}
              expanded={!!expandedGroups.tasks}
              onExpand={expand("tasks")}
            >
              {cap(results.tasks, "tasks").map((t) => {
                const proj = projectById(state, t.projectId);
                const open = !!openItems[t.id];
                const detail = [
                  proj ? proj.name : "no project",
                  t.due ? `due ${formatDay(t.due)}` : null,
                  t.waitingOn ? `waiting on ${t.waitingOn}` : null,
                ]
                  .filter(Boolean)
                  .join(" · ");
                return (
                  <div key={t.id} className="border-b border-gtc-line/60">
                    <button
                      type="button"
                      className={ROW_BTN}
                      onClick={toggleItem(t.id)}
                      aria-expanded={open}
                    >
                      <span className={cn(META, "w-16 shrink-0")}>{t.status}</span>
                      <span className="min-w-0 flex-1 truncate text-[0.85rem] text-gtc-text">
                        <Highlight text={t.title} query={q} />
                      </span>
                    </button>
                    {open && <div className={cn(META, "pb-2 pl-[4.75rem]")}>{detail}</div>}
                  </div>
                );
              })}
            </Group>

            <Group
              label="Notes"
              count={results.notes.length}
              expanded={!!expandedGroups.notes}
              onExpand={expand("notes")}
            >
              {cap(results.notes, "notes").map((n) => {
                const open = !!openItems[n.id];
                return (
                  <div key={n.id} className="border-b border-gtc-line/60">
                    <button
                      type="button"
                      className={ROW_BTN}
                      onClick={toggleItem(n.id)}
                      aria-expanded={open}
                    >
                      <span className="shrink-0 text-[0.85rem] text-gtc-text">
                        <Highlight text={n.title} query={q} />
                      </span>
                      {!open && (
                        <span className="min-w-0 flex-1 truncate text-[0.8rem] text-gtc-muted">
                          <Highlight text={snippet(n.body, q)} query={q} />
                        </span>
                      )}
                    </button>
                    {open && (
                      <p className="max-w-[68ch] whitespace-pre-line pb-2.5 text-[0.85rem] leading-relaxed text-gtc-text/90">
                        <Highlight text={n.body} query={q} />
                      </p>
                    )}
                  </div>
                );
              })}
            </Group>

            <Group
              label="Reminders"
              count={results.reminders.length}
              expanded={!!expandedGroups.reminders}
              onExpand={expand("reminders")}
            >
              {cap(results.reminders, "reminders").map((r) => (
                <div
                  key={r.id}
                  className="-mx-2 flex items-center gap-3 border-b border-gtc-line/60 px-2 py-2"
                >
                  <Bell className="h-3.5 w-3.5 shrink-0 text-gtc-muted" aria-hidden />
                  <span className="min-w-0 flex-1 truncate text-[0.85rem] text-gtc-text">
                    <Highlight text={r.text} query={q} />
                  </span>
                  <span className={cn(META, "shrink-0")}>
                    {formatDay(r.remindAt.slice(0, 10))} {r.remindAt.slice(11, 16)}
                  </span>
                </div>
              ))}
            </Group>

            <Group
              label="Inbox"
              count={results.inbox.length}
              expanded={!!expandedGroups.inbox}
              onExpand={expand("inbox")}
            >
              {cap(results.inbox, "inbox").map((i) => (
                <div key={i.id} className="border-b border-gtc-line/60">
                  <button
                    type="button"
                    className={ROW_BTN}
                    onClick={() => dispatch({ type: "SET_VIEW", view: "inbox" })}
                  >
                    <span className="min-w-0 flex-1 truncate text-[0.85rem] text-gtc-text">
                      <Highlight text={snippet(i.raw, q)} query={q} />
                    </span>
                    <span className={cn(META, "shrink-0")}>{i.status}</span>
                  </button>
                </div>
              ))}
            </Group>
          </div>
        )}
      </div>
    </div>
  );
}
