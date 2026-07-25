import type { ActivityEntry, Project } from "@/domain/types";
import type { AppState } from "./AppStore";

export function projectById(state: AppState, id: string | undefined | null): Project | undefined {
  return state.projects.find((p) => p.id === id);
}

/** Projects visible under the current filters (rail + timeline rows). */
export function visibleProjects(state: AppState): Project[] {
  let list = state.projects;
  if (!state.filters.showCompleted) list = list.filter((p) => p.status !== "completed");
  if (state.filters.projectIds) {
    const allowed = new Set(state.filters.projectIds);
    list = list.filter((p) => allowed.has(p.id));
  }
  return list;
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
