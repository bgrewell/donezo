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
  /** Address the invite was emailed to, absent for a bare code. */
  email?: string;
}

/** A freshly minted invite — the only place the plaintext code exists. */
export interface CreatedInvite {
  id: string;
  code: string;
  codePrefix: string;
  expiresAt: string;
  /** Address it was emailed to, when created with one. */
  email?: string;
  /** True when the invite email was accepted by the mail server. */
  sent?: boolean;
  /** Present when the invite was created but its email could not be sent. */
  warning?: string;
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
  password: string,
  email: string
): Promise<ApiUser> {
  const body = { username, displayName, password, email };
  return (await api.post<{ user: ApiUser }>("/api/auth/setup", body)).user;
}

export async function login(username: string, password: string): Promise<ApiUser> {
  return (await api.post<{ user: ApiUser }>("/api/auth/login", { username, password })).user;
}

/** Redeem an invite code into a fresh member account (plus its "main"
 *  space and a session). Every unusable code answers a uniform 403;
 *  a taken username is a 409, a taken email likewise. */
export async function register(
  code: string,
  username: string,
  displayName: string,
  password: string,
  email: string
): Promise<ApiUser> {
  const body = { code, username, displayName, password, email };
  return (await api.post<{ user: ApiUser }>("/api/auth/register", body)).user;
}

export function logout(): Promise<void> {
  return api.post<void>("/api/auth/logout");
}

/** Request a password-reset email. The server answers the same whether or not
 *  the address is on file, so this resolves for any syntactically valid email
 *  and never reveals whether an account exists. */
export async function forgotPassword(email: string): Promise<void> {
  await api.post<{ message: string }>("/api/auth/forgot-password", { email });
}

/** Spend a reset token from an emailed link and set a new password. On success
 *  the server issues a session, so the caller is logged straight in. */
export async function resetPassword(token: string, password: string): Promise<ApiUser> {
  return (await api.post<{ user: ApiUser }>("/api/auth/reset-password", { token, password })).user;
}

// ─── Invites (admin only) ─────────────────────────────────────────────────

/** Mint an invite code, optionally emailing it to an address. The plaintext
 *  code appears only in this response, whether or not it was also emailed. */
export async function createInvite(
  expiresInDays?: number,
  email?: string
): Promise<CreatedInvite> {
  const body: { expiresInDays?: number; email?: string } = {};
  if (expiresInDays !== undefined) body.expiresInDays = expiresInDays;
  if (email) body.email = email;
  const payload = Object.keys(body).length > 0 ? body : undefined;
  return (await api.post<{ invite: CreatedInvite }>("/api/invites", payload)).invite;
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

/** A channel reminders can be delivered on. */
export type NotifyChannel = "email" | "sms";

/** What one channel can do on this instance. Configuring channels is the
 *  operator's job (environment variables, not a settings screen), so the UI
 *  reads this to explain why a channel is unavailable instead of accepting a
 *  destination that would never be delivered to. */
export interface NotifyChannelStatus {
  channel: NotifyChannel;
  configured: boolean;
  /** Describes the provider for the operator; never a credential. */
  provider?: string;
}

/** One destination the current user's reminders can be delivered to.
 *
 *  `verifiedAt` is the only thing that makes it deliverable: until its owner
 *  has entered the code sent to it, nothing is ever sent there. */
export interface NotifyContact {
  id: string;
  channel: NotifyChannel;
  address: string;
  label: string;
  verifiedAt?: string;
  createdAt: string;
  /** A verification code is outstanding (the code itself is never returned). */
  pendingCode: boolean;
}

/** What POST /api/notify/contacts answers: the row, plus a warning when it
 *  was stored but the code could not be sent. */
export interface CreatedNotifyContact {
  contact: NotifyContact;
  warning?: string;
}

/** Which channels this instance can deliver on. */
export async function fetchNotifyStatus(): Promise<NotifyChannelStatus[]> {
  return (await api.get<{ channels: NotifyChannelStatus[] }>("/api/notify/status")).channels;
}

/** The current user's delivery destinations. Each user sees only their own. */
export async function fetchNotifyContacts(): Promise<NotifyContact[]> {
  return (await api.get<{ contacts: NotifyContact[] }>("/api/notify/contacts")).contacts;
}

/** Add a destination. The server sends it a verification code as part of
 *  this call — the two are one action to the person doing it. */
export function createNotifyContact(
  channel: NotifyChannel,
  address: string,
  label: string
): Promise<CreatedNotifyContact> {
  return api.post<CreatedNotifyContact>("/api/notify/contacts", { channel, address, label });
}

/** Send a fresh verification code (throttled server-side to once a minute). */
export function sendNotifyContactCode(id: string): Promise<{ contact: NotifyContact }> {
  return api.post<{ contact: NotifyContact }>(
    `/api/notify/contacts/${encodeURIComponent(id)}/code`,
    {}
  );
}

/** Confirm a destination with the code that was sent to it. */
export function verifyNotifyContact(
  id: string,
  code: string
): Promise<{ contact: NotifyContact }> {
  return api.post<{ contact: NotifyContact }>(
    `/api/notify/contacts/${encodeURIComponent(id)}/verify`,
    { code }
  );
}

/** Remove a destination. */
export function deleteNotifyContact(id: string): Promise<void> {
  return api.del(`/api/notify/contacts/${encodeURIComponent(id)}`);
}

/** Counts returned by DELETE project: the rows moved to the trash.
 *
 *  No detached counts since the trash landed: reminders and inbox items keep
 *  their project link, because a trashed project is still there to point at.
 *  They read as unfiled until it is restored or purged. */
export interface ProjectCascade {
  project: number;
  activities: number;
  tasks: number;
  notes: number;
}

/** One entry in the trash. */
export interface TrashItem {
  /** project | activity | task | note | reminder | inbox_item */
  entity: string;
  id: string;
  /** The row's one-line description, so the view can say what it was. */
  label: string;
  /** RFC 3339 UTC instant — convert for display, do not slice. */
  deletedAt: string;
  /** Groups everything one delete removed. */
  batch: string;
  /** How many rows share the batch; above 1 only for a project that took
   *  content with it, which is exactly when it is worth saying so. */
  batchSize: number;
}

/** GET /api/spaces/{id}/trash */
export async function fetchTrash(spaceId: string): Promise<TrashItem[]> {
  return (
    await api.get<{ trash: TrashItem[] }>(`/api/spaces/${encodeURIComponent(spaceId)}/trash`)
  ).trash;
}

/** Restore an item and everything deleted alongside it. */
export async function restoreTrashItem(
  spaceId: string,
  entity: string,
  id: string
): Promise<number> {
  return (
    await api.post<{ restored: number }>(
      `/api/spaces/${encodeURIComponent(spaceId)}/trash/${encodeURIComponent(entity)}/${encodeURIComponent(id)}/restore`,
      {}
    )
  ).restored;
}

/** Permanently remove an item and its batch. */
export async function purgeTrashItem(
  spaceId: string,
  entity: string,
  id: string
): Promise<number> {
  return (
    await request<{ purged: number }>(
      "DELETE",
      `/api/spaces/${encodeURIComponent(spaceId)}/trash/${encodeURIComponent(entity)}/${encodeURIComponent(id)}`
    )
  ).purged;
}

/** Permanently remove everything in the trash. */
export async function emptyTrash(spaceId: string): Promise<number> {
  return (
    await api.post<{ purged: number }>(`/api/spaces/${encodeURIComponent(spaceId)}/trash/empty`, {})
  ).purged;
}

/** DELETE /api/spaces/{id}/projects/{pid} — move a project and the content it
 *  owns to the trash, restorable as one batch. */
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

/** GET /api/spaces/{id}/revision — a counter that moves whenever anything in
 *  the space changes, from any client or over MCP.
 *
 *  Polled every few seconds by every open tab, so it is deliberately the
 *  cheapest call in the API: the server answers it from memory without
 *  touching the space database. Compare it to the last value seen and refetch
 *  state only when it moves. It is meaningful only within one donezod
 *  process — a restart returns it to zero, which reads as a change and costs
 *  one refetch. */
/** GET /api/instance — what this donezod is.
 *
 *  `version` is absent when the operator runs with --hide-version, so callers
 *  must treat "no version" as a normal answer rather than a failure. */
export interface InstanceInfo {
  version?: string;
}

export function fetchInstance(): Promise<InstanceInfo> {
  return api.get<InstanceInfo>("/api/instance");
}

export async function fetchSpaceRevision(spaceId: string): Promise<number> {
  const res = await api.get<{ revision: number }>(
    `/api/spaces/${encodeURIComponent(spaceId)}/revision`
  );
  return res.revision;
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
  /** IANA zone this user's calendar days are read in, reported by the browser
   *  rather than chosen. It is what lets the server date an agent's write the
   *  same day this browser would — without it "today" is the server's idea of
   *  today, which is a day out for part of every evening. */
  timezone?: string;
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
  /** This user's own wording for model prompts, keyed by prompt id. Each
   *  value replaces that prompt's tunable body only — the fixed core is
   *  appended regardless. An empty value clears the override, restoring the
   *  instance's wording. Keys not mentioned are left alone. */
  prompts?: Record<string, string>;
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
  /** Prompts this instance serves, listed whether or not a model is
   *  configured. */
  prompts: LLMPrompt[];
}

/** One prompt, as the settings UI needs to render it. */
export interface LLMPrompt {
  id: string;
  description: string;
  /** The wording in effect for this user — their own if they have saved one,
   *  otherwise the instance's. */
  body: string;
  /** What `body` falls back to when the user's own wording is cleared. */
  default: string;
  /** Appended to whatever `body` ends up being, and not editable. Shown so
   *  the constraint is visible rather than a surprise. */
  core: string;
  /** Whether `body` came from this user's own settings. */
  customized: boolean;
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

/** How much of one entity exists, and how much of it is recent. */
export interface EntityUsage {
  total: number;
  last7: number;
  last30: number;
  last90: number;
}

/** How many rows have an optional field filled in. The ratio is the point:
 *  a field nobody fills is a feature nobody uses. */
export interface FieldAdoption {
  total: number;
  set: number;
}

/** One person's use of donezo, in counts.
 *
 *  Counts only, by design — see the server's store.UsageStats. There are no
 *  project or space identifiers here, because a project's id is its name
 *  slugified, which would make "usage statistics" a way to read the contents
 *  of somebody's private space. */
export interface UserUsage {
  username: string;
  displayName: string;
  role: string;
  createdAt: string;
  spaces: number;
  archivedSpaces: number;
  projects: EntityUsage;
  activities: EntityUsage;
  tasks: EntityUsage;
  notes: EntityUsage;
  reminders: EntityUsage;
  inbox: EntityUsage;
  fields: Record<string, FieldAdoption>;
  activityTypes: Record<string, number>;
  projectStatuses: Record<string, number>;
  inboxStatuses: Record<string, number>;
  tasksOpen: number;
  tasksDone: number;
  tasksOverdue: number;
  altNextActionsUsed: number;
  distinctTags: number;
  apiTokens: number;
  apiTokensUsed: number;
  notifyContacts: number;
  notifyContactsVerified: number;
  lastWriteAt?: string;
}

/** The whole instance: every user, the totals, and an honest list of what
 *  the stored data cannot answer. */
export interface InstanceUsage {
  generatedAt: string;
  users: UserUsage[];
  totals: UserUsage;
  activeLast30: number;
  notDerivable: string[];
}

/** GET /api/admin/usage — admin only; the server enforces that, not the UI. */
export function fetchUsageStats(): Promise<InstanceUsage> {
  return api.get<InstanceUsage>("/api/admin/usage");
}
