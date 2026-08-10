import * as React from "react";
import { cn } from "@grewelltech/console";

import {
  ApiError,
  fetchUsageStats,
  type FieldAdoption,
  type InstanceUsage,
  type UserUsage,
} from "@/api/client";
import { useSession } from "@/components/auth/session";
import { relativeFromInstant } from "@/lib/time";

/** The fields worth showing, and what each one answers. Ordered so the
 *  product questions read top to bottom rather than by entity. */
const FIELD_ROWS: { key: string; label: string; question: string }[] = [
  { key: "nextAction", label: "Next action", question: "Is the one-next-action idea used?" },
  { key: "resumeContext", label: "Resume note", question: "Is resume context written, or dead weight?" },
  { key: "purpose", label: "Purpose", question: "Do projects get a why?" },
  { key: "outcome", label: "Desired outcome", question: "Do projects get a done?" },
  { key: "currentFocus", label: "Current focus", question: "Is the thread named?" },
  { key: "waitingOn", label: "Waiting on", question: "Real field, or is waiting just a status?" },
  { key: "taskDue", label: "Task due date", question: "Do people date their tasks?" },
  { key: "taskDetails", label: "Task details", question: "Does the details field earn its place?" },
  { key: "taskProject", label: "Task → project", question: "Do tasks get filed, or float?" },
  { key: "effortHours", label: "Effort hours", question: "Is effort tracked?" },
  { key: "activityDetails", label: "Activity details", question: "Is the long form used?" },
  { key: "activityLinks", label: "Activity links", question: "Are links a real feature?" },
  { key: "projectTags", label: "Project tags", question: "Are tags a real feature?" },
  { key: "noteBody", label: "Note body", question: "Are notes more than a title?" },
  { key: "reminderDetails", label: "Reminder details", question: "Do reminders need a long form?" },
  { key: "reminderDelivered", label: "Reminders delivered", question: "Is delivery reaching anyone?" },
  { key: "inboxSuggestedProject", label: "Inbox suggestion", question: "Does capture guess a project?" },
];

/** A share bar. Deliberately plain: the number is the message and a chart
 *  would only make a ratio harder to read. */
function Share({ adoption }: { adoption?: FieldAdoption }) {
  const total = adoption?.total ?? 0;
  const set = adoption?.set ?? 0;
  const pct = total === 0 ? 0 : Math.round((set / total) * 100);
  return (
    <span className="flex items-center gap-2">
      <span className="h-1 w-[70px] shrink-0 overflow-hidden rounded-gtc bg-gtc-inset">
        <span
          className={cn(
            "block h-full",
            pct === 0 ? "bg-transparent" : pct < 25 ? "bg-gtc-warn" : "bg-gtc-accent"
          )}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className="font-mono text-[0.72rem] tabular-nums text-gtc-text">
        {total === 0 ? "—" : `${pct}%`}
      </span>
      <span className="font-mono text-[0.66rem] text-gtc-muted">
        {total === 0 ? "nothing to measure" : `${set} of ${total}`}
      </span>
    </span>
  );
}

/** A counted entity with its recent windows. */
function CountRow({ label, usage }: { label: string; usage: { total: number; last30: number } }) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="font-mono text-[1.1rem] tabular-nums text-gtc-text">{usage.total}</span>
      <span className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        {label}
      </span>
      <span className="font-mono text-[0.66rem] text-gtc-muted">+{usage.last30} in 30d</span>
    </div>
  );
}

/** A distribution, as "key n" chips. Empty says so rather than rendering
 *  nothing, since an empty row and a missing row read very differently. */
function Distribution({ label, values }: { label: string; values: Record<string, number> }) {
  const entries = Object.entries(values).sort((a, b) => b[1] - a[1]);
  return (
    <div>
      <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        {label}
      </div>
      <div className="mt-1 flex flex-wrap gap-1">
        {entries.length === 0 ? (
          <span className="text-[0.78rem] text-gtc-muted">none yet</span>
        ) : (
          entries.map(([key, n]) => (
            <span
              key={key}
              className="rounded-gtc border border-gtc-line px-1.5 py-[2px] font-mono text-[0.66rem] text-gtc-muted"
            >
              {key} <span className="text-gtc-text">{n}</span>
            </span>
          ))
        )}
      </div>
    </div>
  );
}

/** The figures for one scope — the whole instance, or one person. */
function UsageBody({ usage }: { usage: UserUsage }) {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap gap-x-6 gap-y-2">
        <CountRow label="projects" usage={usage.projects} />
        <CountRow label="activities" usage={usage.activities} />
        <CountRow label="tasks" usage={usage.tasks} />
        <CountRow label="notes" usage={usage.notes} />
        <CountRow label="reminders" usage={usage.reminders} />
        <CountRow label="captures" usage={usage.inbox} />
      </div>

      <div className="flex flex-wrap gap-x-6 gap-y-2 border-t border-gtc-line pt-3 font-mono text-[0.72rem] text-gtc-muted">
        <span>
          tasks <span className="text-gtc-text">{usage.tasksDone}</span> done /{" "}
          <span className="text-gtc-text">{usage.tasksOpen}</span> open /{" "}
          <span className={usage.tasksOverdue > 0 ? "text-gtc-warn" : "text-gtc-text"}>
            {usage.tasksOverdue}
          </span>{" "}
          overdue
        </span>
        <span>
          spaces <span className="text-gtc-text">{usage.spaces}</span>
          {usage.archivedSpaces > 0 && ` (${usage.archivedSpaces} archived)`}
        </span>
        <span>
          distinct tags <span className="text-gtc-text">{usage.distinctTags}</span>
        </span>
        <span>
          alternates used <span className="text-gtc-text">{usage.altNextActionsUsed}</span>
        </span>
        <span>
          MCP tokens <span className="text-gtc-text">{usage.apiTokensUsed}</span> used of{" "}
          <span className="text-gtc-text">{usage.apiTokens}</span>
        </span>
        <span>
          reminder destinations{" "}
          <span className="text-gtc-text">{usage.notifyContactsVerified}</span> confirmed of{" "}
          <span className="text-gtc-text">{usage.notifyContacts}</span>
        </span>
      </div>

      <div className="grid gap-4 border-t border-gtc-line pt-3 sm:grid-cols-3">
        <Distribution label="Activity types" values={usage.activityTypes} />
        <Distribution label="Project statuses" values={usage.projectStatuses} />
        <Distribution label="Inbox" values={usage.inboxStatuses} />
      </div>

      <div className="border-t border-gtc-line pt-3">
        <div className="pb-1.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          Which fields earn their place
        </div>
        <ul className="m-0 list-none space-y-1 p-0">
          {FIELD_ROWS.map((row) => (
            <li key={row.key} className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
              <span className="w-[150px] shrink-0 font-sans text-[0.8rem] text-gtc-text">
                {row.label}
              </span>
              <Share adoption={usage.fields[row.key]} />
              <span className="min-w-0 flex-1 truncate text-[0.72rem] text-gtc-muted">
                {row.question}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/**
 * Usage statistics (#45), derived half.
 *
 * Everything here is computed from what is already stored — no event log.
 * That is why the "what this cannot answer" list at the bottom is not an
 * apology but part of the panel: an absent figure reads as "nobody does
 * this", and two of the most interesting questions are simply not
 * answerable from stored rows yet.
 *
 * Nothing here is anyone's content. The server sends counts and
 * distributions only, and never a project or space identifier — ids in
 * donezo are slugified names.
 */
export function UsageSection() {
  const { sessionExpired } = useSession();
  const [stats, setStats] = React.useState<InstanceUsage | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  // null is the instance total; a username scopes to that person.
  const [scope, setScope] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    fetchUsageStats()
      .then((s) => !cancelled && setStats(s))
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) sessionExpired();
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [sessionExpired]);

  if (error) {
    return (
      <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
        ▸ {error}
      </p>
    );
  }
  if (!stats) {
    return <p className="m-0 font-mono text-[0.72rem] text-gtc-muted">loading…</p>;
  }

  const active = scope === null ? stats.totals : stats.users.find((u) => u.username === scope);

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="font-mono text-[0.72rem] text-gtc-muted">
          <span className="text-gtc-text">{stats.users.length}</span> accounts ·{" "}
          <span className="text-gtc-text">{stats.activeLast30}</span> wrote something in 30d
        </span>
        <span className="font-mono text-[0.66rem] text-gtc-muted">
          as of {relativeFromInstant(stats.generatedAt)}
        </span>
      </div>

      <div className="flex flex-wrap gap-1" role="group" aria-label="Usage scope">
        {[{ username: null, label: "Everyone" }, ...stats.users.map((u) => ({
          username: u.username,
          label: u.displayName || u.username,
        }))].map((option) => {
          const selected = option.username === scope;
          return (
            <button
              key={option.username ?? "__all__"}
              type="button"
              aria-pressed={selected}
              onClick={() => setScope(option.username)}
              className={cn(
                "rounded-gtc border px-2 py-1 font-mono text-[0.66rem] uppercase tracking-label",
                "outline-none transition-colors focus-visible:shadow-gtc-focus",
                selected
                  ? "border-gtc-accent bg-gtc-tint-accent text-gtc-accent"
                  : "border-gtc-line text-gtc-muted hover:text-gtc-text"
              )}
            >
              {option.label}
            </button>
          );
        })}
      </div>

      {active && (
        <>
          {scope !== null && (
            <p className="m-0 font-mono text-[0.66rem] text-gtc-muted">
              {active.role} · joined {relativeFromInstant(active.createdAt)}
              {active.lastWriteAt && ` · last wrote ${active.lastWriteAt.slice(0, 10)}`}
            </p>
          )}
          <UsageBody usage={active} />
        </>
      )}

      <div className="border-t border-gtc-line pt-3">
        <div className="pb-1.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
          What these numbers cannot say
        </div>
        <ul className="m-0 list-none space-y-1 p-0">
          {stats.notDerivable.map((line) => (
            <li key={line} className="text-[0.78rem] text-gtc-muted">
              — {line}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
