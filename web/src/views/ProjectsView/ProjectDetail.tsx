import { Button, Checkbox, cn, SectionLabel } from "@grewelltech/console";
import { ExternalLink } from "lucide-react";

import type { ActivityLink, Project, TaskItem } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { activitiesForProject } from "@/state/selectors";
import { formatDay, relativeFromToday, todayISO } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";
import { ActivityTypeIcon } from "@/components/common/activityTypes";
import { MiniPulse } from "./MiniPulse";

/** First sentence of a details blob, for decision summaries. Punctuation
 *  inside numbers ("p99 1.2ms") does not end a sentence. */
function firstSentence(text: string): string {
  const m = text.match(/^[\s\S]*?[.!?](?=\s|$)/);
  return (m ? m[0] : text).trim();
}

/** Body preview for note rows. */
function excerpt(text: string, max = 100): string {
  return text.length <= max ? text : `${text.slice(0, max).trimEnd()}…`;
}

function DefRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">{label}</dt>
      <dd className="mt-0.5 font-sans text-[0.85rem] leading-relaxed text-gtc-text">{value}</dd>
    </div>
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

function TaskRow({ task, onDone }: { task: TaskItem; onDone: () => void }) {
  const waiting = task.status === "waiting";
  return (
    <div className="flex items-center gap-3 py-2">
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
      {task.due && <DueChip due={task.due} />}
    </div>
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
          <ProjectMark color={project.color} size={12} />
          <h2 className="font-sans text-[1.15rem] font-semibold leading-none text-gtc-text">
            {project.name}
          </h2>
          <StatusBadge status={project.status} />
          {project.tags.map((tag) => (
            <span
              key={tag}
              className="rounded-gtc border border-gtc-line px-1.5 py-[2px] font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted"
            >
              {tag}
            </span>
          ))}
          {project.waitingOn && (
            <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-warn">
              waiting on: {project.waitingOn}
            </span>
          )}
        </div>
      </div>

      {/* Orientation block */}
      <div className="grid gap-6 border-b border-gtc-line pb-6 md:grid-cols-2">
        <dl className="space-y-3.5">
          <DefRow label="Purpose" value={project.purpose} />
          <DefRow label="Desired outcome" value={project.outcome} />
          <DefRow label="Current focus" value={project.currentFocus} />
        </dl>

        <div className="space-y-4">
          <div className="border-l-2 border-gtc-accent bg-gtc-tint-accent px-3 py-2">
            <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-accent">
              Resume here
            </div>
            <p className="mt-1 font-sans text-[0.85rem] leading-relaxed text-gtc-text">
              {project.resumeContext}
            </p>
          </div>

          <div>
            <div className="flex items-center justify-between gap-3">
              <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                Next action
              </span>
              <Button
                size="sm"
                onClick={() => dispatch({ type: "SET_QUICK_CAPTURE", open: true })}
              >
                Log progress
              </Button>
            </div>
            <p className="mt-1 font-sans text-[0.95rem] text-gtc-text">{project.nextAction}</p>
            {project.altNextActions.slice(0, 2).map((alt) => (
              <div key={alt} className="mt-1.5 flex items-baseline gap-2">
                <span className="shrink-0 font-mono text-[0.6rem] uppercase tracking-label text-gtc-muted">
                  alt
                </span>
                <span className="font-sans text-[0.8rem] text-gtc-muted">{alt}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Pulse */}
      <section>
        <SectionLabel className="mb-3 mt-0">Pulse</SectionLabel>
        <MiniPulse
          project={project}
          entries={acts}
          showPlanned={state.filters.showPlanned}
          onSelectEntry={selectActivity}
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
        {openTasks.length === 0 ? (
          <p className="font-sans text-[0.85rem] text-gtc-muted">No open tasks.</p>
        ) : (
          <div className="divide-y divide-gtc-line">
            {openTasks.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                onDone={() =>
                  dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "done" } })
                }
              />
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
                  <div key={n.id} className="py-2">
                    <div className="font-sans text-[0.85rem] font-medium text-gtc-text">
                      {n.title}
                    </div>
                    <p className="font-sans text-[0.8rem] text-gtc-muted">{excerpt(n.body)}</p>
                  </div>
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
    </div>
  );
}
