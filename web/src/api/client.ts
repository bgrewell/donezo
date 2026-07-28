/**
 * Tiny typed fetch wrapper for the donezod HTTP API.
 *
 * Every response body is JSON; failures use the canonical {"error": msg}
 * envelope, surfaced here as a thrown ApiError carrying the HTTP status.
 * Requests ride the session cookie (credentials: "same-origin") — in dev
 * the vite proxy forwards /api to donezod, so everything is same-origin.
 */

import type {
  ActivityEntry,
  InboxItem,
  NoteItem,
  Project,
  Reminder,
  Space,
  TaskItem,
} from "@/domain/types";

/** An API failure: HTTP status plus the server's calm error message. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/** GET /api/auth/status. */
export interface AuthStatus {
  needsSetup: boolean;
  authenticated: boolean;
}

/** The authenticated user, as served by the auth endpoints. */
export interface ApiUser {
  id: number;
  username: string;
  displayName: string;
  /** "admin" (the instance owner) or "member" (invited accounts). */
  role: "admin" | "member";
  createdAt: string;
}

/** One invite in the admin list (GET /api/invites) — never the code. */
export interface Invite {
  id: string;
  /** First characters of the code, for recognizing it in the list. */
  codePrefix: string;
  /** Lifecycle state derived by the server at list time. */
  status: "active" | "used" | "expired" | "revoked";
  createdBy: string;
  createdAt: string;
  expiresAt: string;
  usedBy?: string;
  usedAt?: string;
  revokedAt?: string;
}

/** A freshly minted invite — the only place the plaintext code exists. */
export interface CreatedInvite {
  id: string;
  code: string;
  codePrefix: string;
  expiresAt: string;
}

/** GET /api/spaces/{id}/state — the full content of one space. */
export interface SpaceData {
  projects: Project[];
  activities: ActivityEntry[];
  tasks: TaskItem[];
  notes: NoteItem[];
  reminders: Reminder[];
  inbox: InboxItem[];
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    // fetch itself only rejects on network-level trouble.
    throw new ApiError(0, "can't reach the server");
  }

  let data: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null; // non-JSON body (proxy error page etc.)
    }
  }

  if (!res.ok) {
    const envelope = data as { error?: unknown } | null;
    const message =
      envelope && typeof envelope.error === "string"
        ? envelope.error
        : `request failed (${res.status})`;
    throw new ApiError(res.status, message);
  }
  return data as T;
}

/** Method-shaped helpers; paths are absolute ("/api/…"). */
export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  patch: <T>(path: string, body: unknown) => request<T>("PATCH", path, body),
  del: (path: string) => request<void>("DELETE", path),
};

// ─── Typed endpoint helpers ───────────────────────────────────────────────

export function fetchAuthStatus(): Promise<AuthStatus> {
  return api.get<AuthStatus>("/api/auth/status");
}

export async function fetchMe(): Promise<ApiUser> {
  return (await api.get<{ user: ApiUser }>("/api/auth/me")).user;
}

export async function setup(
  username: string,
  displayName: string,
  password: string
): Promise<ApiUser> {
  const body = { username, displayName, password };
  return (await api.post<{ user: ApiUser }>("/api/auth/setup", body)).user;
}

export async function login(username: string, password: string): Promise<ApiUser> {
  return (await api.post<{ user: ApiUser }>("/api/auth/login", { username, password })).user;
}

/** Redeem an invite code into a fresh member account (plus its "main"
 *  space and a session). Every unusable code answers a uniform 403;
 *  a taken username is a 409. */
export async function register(
  code: string,
  username: string,
  displayName: string,
  password: string
): Promise<ApiUser> {
  const body = { code, username, displayName, password };
  return (await api.post<{ user: ApiUser }>("/api/auth/register", body)).user;
}

export function logout(): Promise<void> {
  return api.post<void>("/api/auth/logout");
}

// ─── Invites (admin only) ─────────────────────────────────────────────────

/** Mint an invite code. The plaintext code appears only in this response. */
export async function createInvite(expiresInDays?: number): Promise<CreatedInvite> {
  const body = expiresInDays !== undefined ? { expiresInDays } : undefined;
  return (await api.post<{ invite: CreatedInvite }>("/api/invites", body)).invite;
}

export async function fetchInvites(): Promise<Invite[]> {
  return (await api.get<{ invites: Invite[] }>("/api/invites")).invites;
}

/** Revoke an invite (idempotent server-side). */
export function revokeInvite(id: string): Promise<void> {
  return api.del(`/api/invites/${encodeURIComponent(id)}`);
}

/** Counts returned by DELETE project: deleted rows plus references that
 *  were detached (kept, project link nulled) rather than removed. */
export interface ProjectCascade {
  project: number;
  activities: number;
  tasks: number;
  notes: number;
  detachedInbox: number;
  detachedReminders: number;
}

/** DELETE /api/spaces/{id}/projects/{pid} — remove a project and all the
 *  content it owns; raw captures and reminders survive, detached. */
export async function deleteProject(
  spaceId: string,
  projectId: string
): Promise<ProjectCascade> {
  return (
    await request<{ deleted: ProjectCascade }>(
      "DELETE",
      `/api/spaces/${encodeURIComponent(spaceId)}/projects/${encodeURIComponent(projectId)}`
    )
  ).deleted;
}

export async function fetchSpaces(): Promise<Space[]> {
  return (await api.get<{ spaces: Space[] }>("/api/spaces")).spaces;
}

export function fetchSpaceState(spaceId: string): Promise<SpaceData> {
  return api.get<SpaceData>(`/api/spaces/${encodeURIComponent(spaceId)}/state`);
}

export async function createSpace(name: string, color: string): Promise<Space> {
  return (await api.post<{ space: Space }>("/api/spaces", { name, color })).space;
}

export async function patchSpace(
  spaceId: string,
  patch: { name?: string; color?: string; position?: number }
): Promise<Space> {
  return (
    await api.patch<{ space: Space }>(`/api/spaces/${encodeURIComponent(spaceId)}`, patch)
  ).space;
}

export async function setSpaceArchived(spaceId: string, archived: boolean): Promise<Space> {
  const verb = archived ? "archive" : "unarchive";
  return (
    await api.post<{ space: Space }>(`/api/spaces/${encodeURIComponent(spaceId)}/${verb}`)
  ).space;
}
