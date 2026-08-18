import type { ActivityEntry, Project, ProjectStatus } from "@/domain/types";
import type { AppState } from "./AppStore";

/** Statuses that close a project's lifecycle (completed or cancelled).
 *  Closed projects hide behind the "Done" toggle and render muted. */
const CLOSED_STATUSES: ReadonlySet<ProjectStatus> = new Set(["completed", "cancelled"]);

/** True when the project is closed — completed or cancelled. */
export function isClosedProject(project: Project): boolean {
  return CLOSED_STATUSES.has(project.status);
}

export function projectById(state: AppState, id: string | undefined | null): Project | undefined {
  return state.projects.find((p) => p.id === id);
}

/** Projects visible under the current filters (rail + timeline rows). The
 *  catch-all sorts last: it is a real project but a bucket of unrelated
 *  chores, so it should never sit at the top as if it were a live thread. */
export function visibleProjects(state: AppState): Project[] {
  let list = state.projects;
  if (!state.filters.showCompleted) list = list.filter((p) => !isClosedProject(p));
  if (state.filters.projectIds) {
    const allowed = new Set(state.filters.projectIds);
    list = list.filter((p) => allowed.has(p.id));
  }
  // Stable partition: keep insertion order, but push the catch-all to the end.
  return [...list].sort((a, b) => Number(a.catchall ?? false) - Number(b.catchall ?? false));
}

/** Activities visible under the current filters (planned layer, types). */
export function filteredActivities(state: AppState): ActivityEntry[] {
  let list = state.activities;
  if (!state.filters.showPlanned) list = list.filter((a) => !a.planned);
  if (state.filters.types) {
    const allowed = new Set(state.filters.types);
    list = list.filter((a) => allowed.has(a.type));
  }
  return list;
}

export function activitiesForProject(state: AppState, projectId: string): ActivityEntry[] {
  return state.activities
    .filter((a) => a.projectId === projectId)
    .sort((a, b) => a.date.localeCompare(b.date));
}

/** Latest non-planned activity date for a project, if any. */
export function latestActivityDate(state: AppState, projectId: string): string | undefined {
  let latest: string | undefined;
  for (const a of state.activities) {
    if (a.projectId !== projectId || a.planned) continue;
    if (!latest || a.date > latest) latest = a.date;
  }
  return latest;
}

export function openTaskCount(state: AppState, projectId: string): number {
  return state.tasks.filter(
    (t) => t.projectId === projectId && (t.status === "open" || t.status === "waiting")
  ).length;
}

export function selectedActivity(state: AppState): ActivityEntry | undefined {
  return state.activities.find((a) => a.id === state.selectedActivityId);
}
