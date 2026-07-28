import * as React from "react";

import type {
  ActivityEntry,
  ActivityType,
  InboxItem,
  ItemKind,
  NoteItem,
  Project,
  Reminder,
  TaskItem,
  ViewId,
  ZoomLevel,
} from "@/domain/types";
import { ApiError, type SpaceData } from "@/api/client";
import { SessionContext } from "@/components/auth/session";
import { anchorForToday, clampAnchor, clampToRange, shiftAnchor } from "@/lib/time";
import { newId } from "@/lib/id";
import { parseHash } from "@/lib/route";
import { syncAction } from "./sync";
// Pure geometry module (no React/DOM) — the store needs the column math to
// seed a today-visible initial anchor on narrow viewports.
import { visibleColumnCount } from "@/views/TimelineView/geometry";

/** Contextual defaults for one quick-capture open (see AppState). */
export interface QuickCapturePreset {
  kind?: ItemKind;
  projectId?: string;
}

/** Timeline display filters. null means "all". */
export interface Filters {
  projectIds: string[] | null;
  types: ActivityType[] | null;
  showPlanned: boolean;
  showCompleted: boolean;
}

export interface AppState {
  projects: Project[];
  activities: ActivityEntry[];
  tasks: TaskItem[];
  notes: NoteItem[];
  reminders: Reminder[];
  inbox: InboxItem[];

  view: ViewId;
  /** Project open in the project-detail view (within Projects). */
  selectedProjectId: string | null;
  /** Activity shown in the inspector; null = inspector closed. */
  selectedActivityId: string | null;
  zoom: ZoomLevel;
  /** Left edge of the visible timeline window, ISO yyyy-MM-dd. */
  anchorDate: string;
  filters: Filters;
  quickCaptureOpen: boolean;
  /** Context the quick-capture dialog should preselect on open: kind (the
   *  Projects list's "+ New project"), and/or project ("Log progress" from
   *  inside a project). null = plain capture, auto-suggest steering. */
  quickCapturePreset: QuickCapturePreset | null;
  navCollapsed: boolean;
  railCollapsed: boolean;
  searchQuery: string;
}

export type AppAction =
  | { type: "SET_VIEW"; view: ViewId }
  | { type: "OPEN_PROJECT"; projectId: string }
  | { type: "CLOSE_PROJECT" }
  | { type: "ADD_PROJECT"; project: Project }
  | { type: "UPDATE_PROJECT"; id: string; patch: Partial<Project> }
  | {
      /** Delete a project and everything it owns, mirroring the server
       *  cascade: activities/tasks/notes go with it; inbox suggestions and
       *  reminders are kept but detached. */
      type: "REMOVE_PROJECT";
      projectId: string;
    }
  | { type: "SELECT_ACTIVITY"; id: string | null }
  | { type: "SET_ZOOM"; zoom: ZoomLevel }
  | { type: "SET_ANCHOR"; date: string }
  | { type: "SHIFT_PERIOD"; dir: 1 | -1 }
  | {
      /** Jump the timeline to today. `visibleColumns` (the whole columns the
       *  measured lane can show) shrinks the past-context offset on narrow
       *  lanes so today's column always lands on screen. */
      type: "JUMP_TODAY";
      visibleColumns?: number;
    }
  | { type: "SET_FILTERS"; patch: Partial<Filters> }
  | { type: "SET_QUICK_CAPTURE"; open: boolean; preset?: QuickCapturePreset }
  | { type: "SET_SEARCH_QUERY"; query: string }
  | { type: "TOGGLE_NAV" }
  | { type: "TOGGLE_RAIL" }
  | { type: "ADD_ACTIVITY"; entry: ActivityEntry }
  | { type: "UPDATE_ACTIVITY"; id: string; patch: Partial<ActivityEntry> }
  | { type: "DELETE_ACTIVITY"; id: string }
  | { type: "ADD_TASK"; task: TaskItem }
  | { type: "UPDATE_TASK"; id: string; patch: Partial<TaskItem> }
  | { type: "ADD_NOTE"; note: NoteItem }
  | { type: "ADD_REMINDER"; reminder: Reminder }
  | { type: "UPDATE_REMINDER"; id: string; patch: Partial<Reminder> }
  | { type: "ADD_INBOX"; item: InboxItem }
  | { type: "UPDATE_INBOX"; id: string; patch: Partial<InboxItem> }
  | {
      /** Atomically convert an inbox item into a structured item. */
      type: "CONVERT_INBOX";
      id: string;
      kind: ItemKind;
      task?: TaskItem;
      note?: NoteItem;
      reminder?: Reminder;
      activity?: ActivityEntry;
      project?: Project;
    };

function patchById<T extends { id: string }>(list: T[], id: string, patch: Partial<T>): T[] {
  return list.map((item) => (item.id === id ? { ...item, ...patch } : item));
}

function reducer(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case "SET_VIEW":
      return {
        ...state,
        view: action.view,
        selectedActivityId: null,
        // Bare "Projects" navigation always lands on the list; deep links
        // into a project go through OPEN_PROJECT instead.
        selectedProjectId:
          action.view === "projects" ? null : state.selectedProjectId,
      };
    case "OPEN_PROJECT":
      return {
        ...state,
        view: "projects",
        selectedProjectId: action.projectId,
        selectedActivityId: null,
      };
    case "CLOSE_PROJECT":
      return { ...state, selectedProjectId: null };
    case "ADD_PROJECT":
      return { ...state, projects: [...state.projects, action.project] };
    case "UPDATE_PROJECT":
      return {
        ...state,
        projects: patchById(state.projects, action.id, action.patch),
      };
    case "REMOVE_PROJECT": {
      const pid = action.projectId;
      // Mirror the server cascade exactly: owned content is deleted,
      // loose references (inbox suggestions, reminders) are detached.
      const selectedActivityRemoved =
        state.selectedActivityId !== null &&
        state.activities.some(
          (a) => a.id === state.selectedActivityId && a.projectId === pid
        );
      return {
        ...state,
        projects: state.projects.filter((p) => p.id !== pid),
        activities: state.activities.filter((a) => a.projectId !== pid),
        tasks: state.tasks.filter((t) => t.projectId !== pid),
        notes: state.notes.filter((n) => n.projectId !== pid),
        reminders: state.reminders.map((r) =>
          r.projectId === pid ? { ...r, projectId: undefined } : r
        ),
        inbox: state.inbox.map((i) =>
          i.suggestedProjectId === pid ? { ...i, suggestedProjectId: undefined } : i
        ),
        selectedProjectId: state.selectedProjectId === pid ? null : state.selectedProjectId,
        selectedActivityId: selectedActivityRemoved ? null : state.selectedActivityId,
      };
    }
    case "SELECT_ACTIVITY":
      return { ...state, selectedActivityId: action.id };
    case "SET_ZOOM":
      // Keep the fine-grained anchor so zoom round trips return exactly;
      // only pull it inside the rendered range. (Applying the per-zoom
      // window clamp here would pin quarter anchors to RANGE_START and
      // strand every quarter -> day round trip at the left wall.)
      return { ...state, zoom: action.zoom, anchorDate: clampToRange(state.anchorDate) };
    case "SET_ANCHOR":
      // Scroll-derived anchors are physical positions — clamp to the range
      // only, so a manual scroll to the far edge is never fought.
      return { ...state, anchorDate: clampToRange(action.date) };
    case "SHIFT_PERIOD": {
      const next = clampAnchor(
        shiftAnchor(state.anchorDate, state.zoom, action.dir),
        state.zoom
      );
      // Clamping can bounce a shift backwards at the walls; treat as no move.
      const moved = action.dir === 1 ? next > state.anchorDate : next < state.anchorDate;
      return moved ? { ...state, anchorDate: next } : state;
    }
    case "JUMP_TODAY":
      return {
        ...state,
        anchorDate: clampAnchor(
          anchorForToday(state.zoom, action.visibleColumns),
          state.zoom,
          action.visibleColumns
        ),
      };
    case "SET_FILTERS":
      return { ...state, filters: { ...state.filters, ...action.patch } };
    case "SET_QUICK_CAPTURE":
      return {
        ...state,
        quickCaptureOpen: action.open,
        quickCapturePreset: action.open ? (action.preset ?? null) : null,
      };
    case "SET_SEARCH_QUERY":
      return { ...state, searchQuery: action.query };
    case "TOGGLE_NAV":
      return { ...state, navCollapsed: !state.navCollapsed };
    case "TOGGLE_RAIL":
      return { ...state, railCollapsed: !state.railCollapsed };
    case "ADD_ACTIVITY":
      return { ...state, activities: [...state.activities, action.entry] };
    case "UPDATE_ACTIVITY":
      return {
        ...state,
        activities: patchById(state.activities, action.id, action.patch),
      };
    case "DELETE_ACTIVITY":
      return {
        ...state,
        activities: state.activities.filter((a) => a.id !== action.id),
        selectedActivityId:
          state.selectedActivityId === action.id ? null : state.selectedActivityId,
      };
    case "ADD_TASK":
      return { ...state, tasks: [...state.tasks, action.task] };
    case "UPDATE_TASK":
      return { ...state, tasks: patchById(state.tasks, action.id, action.patch) };
    case "ADD_NOTE":
      return { ...state, notes: [...state.notes, action.note] };
    case "ADD_REMINDER":
      return { ...state, reminders: [...state.reminders, action.reminder] };
    case "UPDATE_REMINDER":
      return {
        ...state,
        reminders: patchById(state.reminders, action.id, action.patch),
      };
    case "ADD_INBOX":
      return { ...state, inbox: [...state.inbox, action.item] };
    case "UPDATE_INBOX":
      return { ...state, inbox: patchById(state.inbox, action.id, action.patch) };
    case "CONVERT_INBOX": {
      const next: AppState = {
        ...state,
        inbox: patchById(state.inbox, action.id, { status: "converted" }),
      };
      if (action.task) next.tasks = [...next.tasks, action.task];
      if (action.note) next.notes = [...next.notes, action.note];
      if (action.reminder) next.reminders = [...next.reminders, action.reminder];
      if (action.activity) next.activities = [...next.activities, action.activity];
      if (action.project) next.projects = [...next.projects, action.project];
      return next;
    }
  }
}

/** True when the viewport matches the media query (false without a DOM —
 *  initial-render only; deliberately no listeners, resize does not re-collapse). */
function viewportMatches(query: string): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia(query).matches
    : false;
}

/** Estimated whole day columns visible at first paint: viewport width minus
 *  the nav/rail chrome the collapse defaults imply. Initial-render only,
 *  like those defaults — once mounted, the timeline measures its real lane
 *  (JUMP_TODAY re-derives from that measurement). Chrome widths come from
 *  the --dz-* shell metrics, with their current values as fallbacks; the
 *  44px collapsed rail mirrors geometry.railWidth. */
function estimatedInitialDayColumns(
  navCollapsed: boolean,
  railCollapsed: boolean
): number | undefined {
  if (typeof window === "undefined") return undefined;
  const rootVars = window.getComputedStyle(document.documentElement);
  const px = (name: string, fallback: number) => {
    const v = parseFloat(rootVars.getPropertyValue(name));
    return Number.isFinite(v) ? v : fallback;
  };
  const nav = navCollapsed ? px("--dz-nav-w", 52) : px("--dz-nav-w-expanded", 176);
  const rail = railCollapsed ? 44 : px("--dz-rail-w", 260);
  return visibleColumnCount(Math.max(0, window.innerWidth - nav - rail), "day");
}

function initialState(data: SpaceData): AppState {
  // Seed view/project from the URL hash so deep links survive the initial
  // render (effect-based syncing races under StrictMode double-mount).
  const route =
    typeof window !== "undefined" ? parseHash(window.location.hash) : null;
  // Small screens start with the chrome folded away; both remain
  // user-toggleable — these are initial defaults, not live constraints.
  const navCollapsed = viewportMatches("(max-width: 1023px)");
  const railCollapsed = viewportMatches("(max-width: 767px)");
  return {
    projects: data.projects,
    activities: data.activities,
    tasks: data.tasks,
    notes: data.notes,
    reminders: data.reminders,
    inbox: data.inbox,
    view: route?.view ?? "timeline",
    selectedProjectId:
      route?.view === "projects" && route.projectId ? route.projectId : null,
    selectedActivityId: null,
    zoom: "day",
    // Today-visible on any viewport: the anchor's past-context offset
    // shrinks when the estimated first-paint lane fits fewer day columns.
    anchorDate: clampAnchor(
      anchorForToday("day", estimatedInitialDayColumns(navCollapsed, railCollapsed)),
      "day"
    ),
    filters: {
      projectIds: null,
      types: null,
      showPlanned: false,
      showCompleted: false,
    },
    quickCaptureOpen: false,
    quickCapturePreset: null,
    navCollapsed,
    railCollapsed,
    searchQuery: "",
  };
}

const StateContext = React.createContext<AppState | null>(null);
const DispatchContext = React.createContext<React.Dispatch<AppAction> | null>(null);

/** A mutation the server refused (or never received). The optimistic
 *  local change stays applied; the banner offers retry/dismiss. */
export interface SyncFailure {
  id: string;
  action: AppAction;
  message: string;
}

interface SyncErrors {
  failures: SyncFailure[];
  retry: (id: string) => void;
  dismiss: (id: string) => void;
}

const SyncErrorsContext = React.createContext<SyncErrors | null>(null);

export function AppProvider({
  spaceId,
  initialData,
  children,
}: {
  /** The space this store instance is bound to; every synced mutation
   *  targets it. Remounting with a different key/spaceId swaps spaces. */
  spaceId: string;
  /** Server data the store boots from (GET /api/spaces/{id}/state). */
  initialData: SpaceData;
  children: React.ReactNode;
}) {
  const [state, dispatch] = React.useReducer(reducer, initialData, initialState);
  const [failures, setFailures] = React.useState<SyncFailure[]>([]);

  // Optional: present when the store is mounted under AuthGate. A 401
  // means the session died mid-use — retrying with the same dead cookie
  // can never succeed, so the gate must overlay re-auth.
  const session = React.useContext(SessionContext);
  const sessionExpired = session?.sessionExpired;

  // Fire the API request for an action. With failureId set this is a
  // retry: success clears that banner entry, failure refreshes its
  // message in place.
  const runSync = React.useCallback(
    (action: AppAction, failureId?: string) => {
      const request = syncAction(spaceId, action);
      if (!request) return;
      request
        .then(() => {
          if (failureId) {
            setFailures((prev) => prev.filter((f) => f.id !== failureId));
          }
        })
        .catch((err: unknown) => {
          const message = err instanceof Error ? err.message : String(err);
          console.error(`donezo: ${action.type} did not sync — ${message}`, err);
          // Expired session: surface sign-in (the gate keeps this store
          // mounted) and keep the failure queued — after re-auth the
          // banner's Retry re-fires with the fresh cookie and succeeds.
          if (err instanceof ApiError && err.status === 401) sessionExpired?.();
          setFailures((prev) =>
            failureId
              ? prev.map((f) => (f.id === failureId ? { ...f, message } : f))
              : [...prev, { id: newId("sync"), action, message }]
          );
        });
    },
    [spaceId, sessionExpired]
  );

  // Optimistic dispatch: apply locally (unchanged UX), then sync.
  const appDispatch = React.useCallback(
    (action: AppAction) => {
      dispatch(action);
      runSync(action);
    },
    [runSync]
  );

  // Ref mirror so retry/dismiss stay referentially stable without
  // side effects inside state updaters (StrictMode double-invokes those).
  const failuresRef = React.useRef(failures);
  failuresRef.current = failures;

  const retry = React.useCallback(
    (id: string) => {
      const failure = failuresRef.current.find((f) => f.id === id);
      if (failure) runSync(failure.action, failure.id);
    },
    [runSync]
  );
  const dismiss = React.useCallback((id: string) => {
    setFailures((prev) => prev.filter((f) => f.id !== id));
  }, []);

  const syncErrors = React.useMemo<SyncErrors>(
    () => ({ failures, retry, dismiss }),
    [failures, retry, dismiss]
  );

  return (
    <StateContext.Provider value={state}>
      <DispatchContext.Provider value={appDispatch}>
        <SyncErrorsContext.Provider value={syncErrors}>{children}</SyncErrorsContext.Provider>
      </DispatchContext.Provider>
    </StateContext.Provider>
  );
}

export function useAppState(): AppState {
  const ctx = React.useContext(StateContext);
  if (!ctx) throw new Error("useAppState must be used within AppProvider");
  return ctx;
}

export function useAppDispatch(): React.Dispatch<AppAction> {
  const ctx = React.useContext(DispatchContext);
  if (!ctx) throw new Error("useAppDispatch must be used within AppProvider");
  return ctx;
}

/** Failed-mutation banner state: pending failures plus retry/dismiss. */
export function useSyncErrors(): SyncErrors {
  const ctx = React.useContext(SyncErrorsContext);
  if (!ctx) throw new Error("useSyncErrors must be used within AppProvider");
  return ctx;
}
