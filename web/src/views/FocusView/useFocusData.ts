import * as React from "react";

import type { ActivityEntry, Project, TaskItem } from "@/domain/types";
import { useAppState, type AppState } from "@/state/AppStore";
import { latestActivityDate, projectById } from "@/state/selectors";
import { addDaysISO, diffDays, todayISO } from "@/lib/time";

/** How many days ahead still count as "time sensitive". */
const DUE_HORIZON_DAYS = 7;
/** Staleness window (days since last entry) for "recently interrupted". */
const INTERRUPT_MIN_DAYS = 3;
const INTERRUPT_MAX_DAYS = 14;

/** A due task or upcoming reminder in the Time-sensitive list. */
export interface DueRow {
  kind: "task" | "reminder";
  id: string;
  title: string;
  /** ISO yyyy-MM-dd the item is due / resurfaces. */
  due: string;
  /** Due date is already past — rendered as a calm "needs review". */
  overdue: boolean;
  project?: Project;
  /** A reminder this row stands in for, hidden because it mirrors the task.
   *  Acting on the row has to act on both, or completing the task simply
   *  un-hides the reminder and the row appears not to have changed. */
  mirrors?: string;
}

/** A task with status "waiting", with its project resolved. */
export interface WaitingTaskRow {
  task: TaskItem;
  project?: Project;
}

/** An active project gone quiet for 3–14 days, with its latest entry. */
export interface InterruptedRow {
  project: Project;
  latest: ActivityEntry;
}

/** An activity logged today, with its project resolved. */
export interface TodayRow {
  entry: ActivityEntry;
  project?: Project;
}

/** Everything the Focus view renders, derived from app state. */
export interface FocusData {
  /** ISO yyyy-MM-dd for "today". */
  today: string;
  /** Active project with the most recent activity — the current thread. */
  nowProject?: Project;
  /** ISO date the now-project was last touched, if it has any activity. */
  nowLastTouched?: string;
  /** Due/overdue tasks plus upcoming reminders, soonest first. */
  timeSensitive: DueRow[];
  waitingTasks: WaitingTaskRow[];
  /** Projects with status waiting or blocked. */
  waitingProjects: Project[];
  interrupted: InterruptedRow[];
  /** Today's non-planned activities in log order. */
  todayRows: TodayRow[];
  /** Total effort hours logged today. */
  todayEffort: number;
}

/** Latest non-planned activity entry for a project (ties → later entry). */
function latestEntryFor(state: AppState, projectId: string): ActivityEntry | undefined {
  let best: ActivityEntry | undefined;
  for (const a of state.activities) {
    if (a.projectId !== projectId || a.planned) continue;
    if (!best || a.date >= best.date) best = a;
  }
  return best;
}

/** Pure derivation of the Focus view model (exported for testing). */
export function computeFocusData(state: AppState): FocusData {
  const today = todayISO();
  const horizon = addDaysISO(today, DUE_HORIZON_DAYS);

  // NOW — active project with the most recent non-planned activity.
  const activeProjects = state.projects.filter((p) => p.status === "active");
  let nowProject: Project | undefined;
  let nowLastTouched: string | undefined;
  for (const p of activeProjects) {
    const latest = latestActivityDate(state, p.id);
    if (
      !nowProject ||
      (latest !== undefined && (nowLastTouched === undefined || latest > nowLastTouched))
    ) {
      nowProject = p;
      nowLastTouched = latest;
    }
  }

  // TIME SENSITIVE — due tasks (past or within 7 days) + upcoming reminders.
  const timeSensitive: DueRow[] = [];
  for (const t of state.tasks) {
    if (!t.due || t.status === "done" || t.status === "someday") continue;
    if (t.due > horizon) continue;
    timeSensitive.push({
      kind: "task",
      id: t.id,
      title: t.title,
      due: t.due,
      overdue: t.due < today,
      project: projectById(state, t.projectId),
    });
  }
  for (const r of state.reminders) {
    if (r.done) continue;
    const due = r.remindAt.slice(0, 10);
    // Past-due reminders stay visible with the same calm "needs review"
    // treatment as tasks — a missed reminder must not silently vanish.
    if (due > horizon) continue;
    timeSensitive.push({
      kind: "reminder",
      id: r.id,
      title: r.text,
      due,
      overdue: due < today,
      project: projectById(state, r.projectId),
    });
  }
  // A task and a reminder often mirror one commitment ("Email Dan…") —
  // show the task once instead of two near-identical adjacent rows.
  const taskKeys = new Set(
    timeSensitive
      .filter((r) => r.kind === "task")
      .map((r) => `${r.title.trim().toLowerCase()}|${r.project?.id ?? ""}`)
  );
  // Remember which reminder each surviving task row is standing in for. Once
  // Focus could only be read this did not matter; now that a row can be
  // completed there, completing the task alone would drop it from the list,
  // re-admit the suppressed reminder, and redraw a byte-identical row — a
  // Done button that looks like it did nothing.
  const mirroredBy = new Map<string, string>();
  const dedupedTimeSensitive = timeSensitive.filter((r) => {
    if (r.kind !== "reminder") return true;
    const key = `${r.title.trim().toLowerCase()}|${r.project?.id ?? ""}`;
    if (!taskKeys.has(key)) return true;
    mirroredBy.set(key, r.id);
    return false;
  });
  for (const row of dedupedTimeSensitive) {
    if (row.kind !== "task") continue;
    const mirrored = mirroredBy.get(`${row.title.trim().toLowerCase()}|${row.project?.id ?? ""}`);
    if (mirrored) row.mirrors = mirrored;
  }
  dedupedTimeSensitive.sort(
    (a, b) => a.due.localeCompare(b.due) || a.title.localeCompare(b.title)
  );

  // WAITING ON — waiting tasks plus waiting/blocked projects.
  const waitingTasks: WaitingTaskRow[] = state.tasks
    .filter((t) => t.status === "waiting")
    .map((task) => ({ task, project: projectById(state, task.projectId) }));
  const waitingProjects = state.projects.filter(
    (p) => p.status === "waiting" || p.status === "blocked"
  );

  // RECENTLY INTERRUPTED — active projects idle 3–14 days (excluding the
  // current thread, which already sits at the top of the view).
  const interrupted: InterruptedRow[] = [];
  for (const p of activeProjects) {
    if (p.id === nowProject?.id) continue;
    const latest = latestEntryFor(state, p.id);
    if (!latest) continue;
    const age = diffDays(today, latest.date);
    if (age < INTERRUPT_MIN_DAYS || age > INTERRUPT_MAX_DAYS) continue;
    interrupted.push({ project: p, latest });
  }
  interrupted.sort((a, b) => b.latest.date.localeCompare(a.latest.date));

  // TODAY — what actually got logged today.
  const todayRows: TodayRow[] = state.activities
    .filter((a) => a.date === today && !a.planned)
    .map((entry) => ({ entry, project: projectById(state, entry.projectId) }));
  const todayEffort = todayRows.reduce((sum, r) => sum + (r.entry.effortHours ?? 0), 0);

  return {
    today,
    nowProject,
    nowLastTouched,
    timeSensitive: dedupedTimeSensitive,
    waitingTasks,
    waitingProjects,
    interrupted,
    todayRows,
    todayEffort,
  };
}

/** Memoized Focus view model for the current app state. */
export function useFocusData(): FocusData {
  const state = useAppState();
  return React.useMemo(() => computeFocusData(state), [state]);
}
