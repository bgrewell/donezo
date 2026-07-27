/**
 * donezo domain model.
 *
 * Historical activity (what actually happened) is kept separate from
 * planned work (tasks / planned entries). The timeline renders activity by
 * default; planned entries are an optional layer.
 */

/** Primary navigation destinations. */
export type ViewId = "focus" | "timeline" | "inbox" | "projects" | "review" | "search";

/** Keys into the --dz-pj-* project color ramp (see styles/app.css). */
export type ProjectColor =
  | "blue"
  | "green"
  | "tan"
  | "violet"
  | "rose"
  | "orange"
  | "steel";

export type ProjectStatus =
  | "active"
  | "waiting"
  | "blocked"
  | "paused"
  | "completed"
  | "cancelled";

/** A workspace: an isolated set of projects/activities/tasks/notes/
 *  reminders/inbox, stored server-side in its own database. Space colors
 *  key into the same --dz-pj-* ramp as projects. */
export interface Space {
  id: string;
  name: string;
  color: ProjectColor;
  position: number;
  /** ISO datetime when archived; absent while the space is live. */
  archivedAt?: string;
}

export type ActivityType = "work" | "research" | "meeting" | "decision" | "blocker" | "milestone";

/** What a raw capture can become. */
export type ItemKind = "task" | "note" | "reminder" | "activity" | "project";

export interface Project {
  id: string;
  name: string;
  color: ProjectColor;
  /** Why this stream of work exists. */
  purpose: string;
  /** What "done" looks like. */
  outcome: string;
  /** The current thread being pulled. */
  currentFocus: string;
  /** The single next concrete action. */
  nextAction: string;
  /** Up to two alternates when the primary next action is blocked/stale. */
  altNextActions: string[];
  status: ProjectStatus;
  /** Context note for resuming after an interruption. */
  resumeContext: string;
  /** Who/what is being waited on, when status is waiting or blocked. */
  waitingOn?: string;
  tags: string[];
}

export interface ActivityLink {
  label: string;
  url: string;
}

/** Something that actually happened on a project (or is planned to). */
export interface ActivityEntry {
  id: string;
  projectId: string;
  /** Local date, ISO yyyy-MM-dd. */
  date: string;
  type: ActivityType;
  title: string;
  details: string;
  /** Rough effort in hours. */
  effortHours?: number;
  source: "manual" | "capture" | "import";
  tags: string[];
  links: ActivityLink[];
  /** Suggested next concrete action following this entry. */
  nextAction?: string;
  /** True for the optional planned-work layer (future/tentative entries). */
  planned?: boolean;
}

export type TaskStatus = "open" | "waiting" | "someday" | "done";

export interface TaskItem {
  id: string;
  projectId?: string;
  title: string;
  status: TaskStatus;
  /** Due date, ISO yyyy-MM-dd. */
  due?: string;
  /** Who/what this task is waiting on, when status is "waiting". */
  waitingOn?: string;
  /** ISO yyyy-MM-dd. */
  createdAt: string;
}

export interface NoteItem {
  id: string;
  projectId?: string;
  title: string;
  body: string;
  /** ISO yyyy-MM-dd. */
  createdAt: string;
}

export interface Reminder {
  id: string;
  text: string;
  /** When to resurface, ISO datetime. */
  remindAt: string;
  projectId?: string;
  done?: boolean;
}

/** A raw capture that has not been fully classified yet. */
export interface InboxItem {
  id: string;
  raw: string;
  /** ISO datetime. */
  capturedAt: string;
  suggestedKind: ItemKind;
  suggestedProjectId?: string;
  status: "pending" | "converted" | "dismissed";
}

export type ZoomLevel = "day" | "week" | "month" | "quarter";
