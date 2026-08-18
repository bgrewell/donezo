/**
 * donezo domain model.
 *
 * Historical activity (what actually happened) is kept separate from
 * planned work (tasks / planned entries). The timeline renders activity by
 * default; planned entries are an optional layer.
 */

/** Primary navigation destinations. */
export type ViewId =
  | "focus"
  | "timeline"
  | "inbox"
  | "projects"
  | "review"
  | "search"
  | "trash"
  // Settings is a real view with a route, but deliberately not in the nav
  // rail: it is somewhere you visit from your account, not somewhere you
  // work. Being a route is what lets a section be linked to directly.
  | "settings";

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

/** What an existing note can become. Narrower than ItemKind: note-to-note is
 *  an edit dressed up as a conversion, and note-to-project is not a sensible
 *  target — a note is content, not a stream of work. Mirrors noteTargetKinds
 *  in internal/api/mutations.go. */
export type NoteTargetKind = Extract<ItemKind, "task" | "reminder" | "activity">;

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
  /** The space's known catch-all ("Miscellaneous") — where activities logged
   *  with no project in mind land. Created lazily; at most one per space. It is
   *  a real project but treated quietly: sorted last, kept out of momentum. */
  catchall?: boolean;
  /** Manual sort order (ascending; ties break by creation). Set one past the
   *  current max on create; a drag in the Projects list rewrites it. */
  position?: number;
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
  /** The optional long form, mirroring ActivityEntry.details. Always present
   *  on the wire and empty when unset, so nothing has to tell an absent field
   *  from a blank one. The title is what a list shows; this is what it opens
   *  to reveal. */
  details: string;
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
  /** The optional long form. `text` is the short one — a reminder's text plays
   *  the part title plays elsewhere. */
  details: string;
  /** When to resurface, ISO datetime. */
  remindAt: string;
  projectId?: string;
  done?: boolean;
  /** When set, the reminder repeats on this interval and keeps coming back
   *  until it is marked done. Absent is an ordinary one-shot reminder. */
  repeat?: ReminderRepeat;
}

/** The unit of a reminder's recurrence interval. */
export type RepeatUnit = "hour" | "day" | "week";

/** A reminder's recurrence interval: "every `every` `unit`s". */
export interface ReminderRepeat {
  every: number;
  unit: RepeatUnit;
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
