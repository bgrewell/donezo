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

/** What an API token may do over MCP: read the account, or read and write. */
export type ApiTokenScope = "read_only" | "read_write";

/** One API token in the user's list (GET /api/tokens) — never the secret. */
export interface ApiToken {
  id: string;
  name: string;
  /** First characters of the token, for recognizing it in the list. */
  tokenPrefix: string;
  scope: ApiTokenScope;
  createdAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
}

/** A freshly minted API token — the only place the full secret exists. */
export interface CreatedApiToken {
  id: string;
  token: string;
  tokenPrefix: string;
  scope: ApiTokenScope;
  createdAt: string;
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

// ─── API tokens (per-user, for MCP) ───────────────────────────────────────

/** Mint an API token for the current user. The plaintext secret appears
 *  only in this response — the server stores just its hash. */
export function createApiToken(
  name: string,
  scope: ApiTokenScope
): Promise<CreatedApiToken> {
  return api.post<CreatedApiToken>("/api/tokens", { name, scope });
}

/** List the current user's tokens — prefixes and metadata only, never the
 *  secret. Each user sees only their own. */
export async function fetchApiTokens(): Promise<ApiToken[]> {
  return (await api.get<{ tokens: ApiToken[] }>("/api/tokens")).tokens;
}

/** Revoke one of the current user's tokens (idempotent server-side). */
export function revokeApiToken(id: string): Promise<void> {
  return api.del(`/api/tokens/${encodeURIComponent(id)}`);
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

/** A user's stored preferences. Every field is optional: an unset preference
 *  follows the current default rather than being pinned to it. */
export interface UserSettings {
  theme?: string;
  font?: string;
  fontSize?: string;
  /** First-run welcome has been acknowledged. Only ever moves to true — the
   *  server refuses to unset it, so a browser that has not read the server
   *  yet cannot clear what another one recorded. */
  welcomed?: boolean;
  /** The tour was completed or skipped at least once. Same one-way rule. */
  tourDone?: boolean;
  /** Ids of dismissed hint chips. Patching adds to the stored set rather
   *  than replacing it. */
  dismissedHints?: string[];
  /** Write-only intent: clears all three onboarding fields. The deliberate
   *  "show me the first run again" action, kept distinct so that resetting
   *  is never something a stale client does by accident. */
  resetOnboarding?: boolean;
}

/** Read the current user's stored preferences. A user who has never saved
 *  one gets an empty object, not an error. */
export async function fetchUserSettings(): Promise<UserSettings> {
  return (await api.get<{ settings: UserSettings }>("/api/settings")).settings;
}

/** Update some of the current user's preferences and return the full stored
 *  set. Omitted fields are left alone; an empty string clears one. */
export async function saveUserSettings(patch: UserSettings): Promise<UserSettings> {
  return (await api.patch<{ settings: UserSettings }>("/api/settings", patch)).settings;
}

// ─── Language model (optional) ────────────────────────────────────────────

/** What this instance can do with a language model. */
export interface LLMStatus {
  /** Whether a model is configured at all. */
  enabled: boolean;
  provider?: string;
  model?: string;
  /** Built-in prompts, listed whether or not a model is configured. */
  prompts: { id: string; description: string }[];
}

/** Read the instance's model configuration. */
export function fetchLLMStatus(): Promise<LLMStatus> {
  return api.get<LLMStatus>("/api/llm");
}

/** How long the browser waits on a model before giving up. Kept under the
 *  server's own 30s bound so the client sees the server's answer rather
 *  than racing it — this is a backstop for a connection that stalls
 *  without the server ever replying. */
const LLM_CLIENT_TIMEOUT_MS = 45_000;

/** Run a built-in prompt over some text and return the model's version.
 *
 *  Unlike the rest of the client this call is explicitly abortable: a model
 *  round trip is the one request that can legitimately take many seconds,
 *  and the shared request() wrapper has no timeout of its own, so without
 *  this a stalled connection would leave the UI waiting forever. */
export async function rewriteWithLLM(promptId: string, text: string): Promise<string> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), LLM_CLIENT_TIMEOUT_MS);
  try {
    const res = await fetch("/api/llm/rewrite", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ promptId, text }),
      signal: controller.signal,
    });
    const raw = await res.text();
    let data: unknown = null;
    try {
      data = raw ? JSON.parse(raw) : null;
    } catch {
      data = null;
    }
    if (!res.ok) {
      const message =
        data && typeof data === "object" && "error" in data
          ? String((data as { error: unknown }).error)
          : `request failed (${res.status})`;
      throw new ApiError(res.status, message);
    }
    return String((data as { text?: unknown })?.text ?? "");
  } catch (err) {
    if (err instanceof ApiError) throw err;
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new ApiError(0, "the model took too long to respond");
    }
    throw new ApiError(0, "can't reach the server");
  } finally {
    window.clearTimeout(timer);
  }
}
