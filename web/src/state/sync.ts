/**
 * Action → API sync layer.
 *
 * Every mutating AppStore action maps to exactly one donezod request
 * against the active space; view/filter/zoom actions are local-only and
 * map to null. The store applies actions optimistically and fires these
 * requests after the fact — failures surface through the sync-error
 * banner (no rollback machinery in v1).
 */

import { api, deleteProject } from "@/api/client";
import type { AppAction } from "./AppStore";

/** Server-clearable PATCH keys per entity: exactly the fields the backend
 *  models as nullable (the json.RawMessage patch fields in
 *  internal/api/validate.go, mirroring the optional fields in
 *  domain/types.ts). Only these may translate undefined → null. */
const CLEARABLE = {
  project: new Set(["waitingOn"]),
  activity: new Set(["effortHours", "nextAction", "planned"]),
  task: new Set(["projectId", "due", "waitingOn"]),
  note: new Set(["projectId"]),
  reminder: new Set(["projectId", "done"]),
  inbox: new Set(["suggestedProjectId"]),
} as const;

/** JSON body for a PATCH. Keys explicitly set to undefined become null for
 *  the entity's clearable fields, so the server clears them exactly like
 *  the local `{...item, ...patch}` spread does (JSON.stringify would
 *  silently drop them). For every OTHER field the server reads null as
 *  absent-keep — the opposite of the local clear — so an undefined value
 *  there is a caller bug: it is dropped from the request and reported
 *  loudly, instead of diverging in silence. Identity never travels in a
 *  patch. */
function patchBody(
  patch: Record<string, unknown>,
  clearable: ReadonlySet<string>
): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(patch)) {
    if (key === "id") continue;
    if (value !== undefined) {
      body[key] = value;
    } else if (clearable.has(key)) {
      body[key] = null;
    } else {
      console.error(
        `donezo: patch key "${key}" is required server-side and cannot be cleared — ` +
          "dropping it from the request (local state and server disagree until reload)"
      );
    }
  }
  return body;
}

/**
 * Fire the API request matching a store action, scoped to spaceId.
 * Returns null for local-only actions (view state, filters, zoom, …).
 */
export function syncAction(spaceId: string, action: AppAction): Promise<unknown> | null {
  const base = `/api/spaces/${encodeURIComponent(spaceId)}`;
  switch (action.type) {
    case "ADD_PROJECT":
      return api.post(`${base}/projects`, action.project);
    case "UPDATE_PROJECT":
      return api.patch(`${base}/projects/${action.id}`, patchBody(action.patch, CLEARABLE.project));
    case "REMOVE_PROJECT":
      // The store already applied the optimistic local cascade; the
      // server's returned counts are not surfaced (kept simple).
      return deleteProject(spaceId, action.projectId);
    case "ADD_ACTIVITY":
      return api.post(`${base}/activities`, action.entry);
    case "UPDATE_ACTIVITY":
      return api.patch(
        `${base}/activities/${action.id}`,
        patchBody(action.patch, CLEARABLE.activity)
      );
    case "DELETE_ACTIVITY":
      return api.del(`${base}/activities/${action.id}`);
    case "ADD_TASK":
      return api.post(`${base}/tasks`, action.task);
    case "UPDATE_TASK":
      return api.patch(`${base}/tasks/${action.id}`, patchBody(action.patch, CLEARABLE.task));
    case "ADD_NOTE":
      return api.post(`${base}/notes`, action.note);
    case "UPDATE_NOTE":
      return api.patch(`${base}/notes/${action.id}`, patchBody(action.patch, CLEARABLE.note));
    case "DELETE_NOTE":
      return api.del(`${base}/notes/${action.id}`);
    case "ADD_REMINDER":
      return api.post(`${base}/reminders`, action.reminder);
    case "UPDATE_REMINDER":
      return api.patch(
        `${base}/reminders/${action.id}`,
        patchBody(action.patch, CLEARABLE.reminder)
      );
    case "ADD_INBOX":
      return api.post(`${base}/inbox`, action.item);
    case "UPDATE_INBOX":
      return api.patch(`${base}/inbox/${action.id}`, patchBody(action.patch, CLEARABLE.inbox));
    case "CONVERT_INBOX": {
      const body: Record<string, unknown> = { kind: action.kind };
      if (action.task) body.task = action.task;
      if (action.note) body.note = action.note;
      if (action.reminder) body.reminder = action.reminder;
      if (action.activity) body.activity = action.activity;
      if (action.project) body.project = action.project;
      return api.post(`${base}/inbox/${action.id}/convert`, body);
    }
    case "CONVERT_NOTE": {
      const body: Record<string, unknown> = { kind: action.kind };
      if (action.task) body.task = action.task;
      if (action.reminder) body.reminder = action.reminder;
      if (action.activity) body.activity = action.activity;
      return api.post(`${base}/notes/${action.id}/convert`, body);
    }
    default:
      return null;
  }
}
