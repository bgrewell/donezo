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
import {
  ACTIVITIES,
  INBOX_ITEMS,
  NOTES,
  PROJECTS,
  REMINDERS,
  TASKS,
} from "@/domain/mockData";
import { anchorForToday, shiftAnchor } from "@/lib/time";
import { parseHash } from "@/lib/route";

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
  navCollapsed: boolean;
  railCollapsed: boolean;
  searchQuery: string;
}

export type AppAction =
  | { type: "SET_VIEW"; view: ViewId }
  | { type: "OPEN_PROJECT"; projectId: string }
  | { type: "CLOSE_PROJECT" }
  | { type: "ADD_PROJECT"; project: Project }
  | { type: "SELECT_ACTIVITY"; id: string | null }
  | { type: "SET_ZOOM"; zoom: ZoomLevel }
  | { type: "SET_ANCHOR"; date: string }
  | { type: "SHIFT_PERIOD"; dir: 1 | -1 }
  | { type: "JUMP_TODAY" }
  | { type: "SET_FILTERS"; patch: Partial<Filters> }
  | { type: "SET_QUICK_CAPTURE"; open: boolean }
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
      return { ...state, view: action.view, selectedActivityId: null };
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
    case "SELECT_ACTIVITY":
      return { ...state, selectedActivityId: action.id };
    case "SET_ZOOM":
      return { ...state, zoom: action.zoom };
    case "SET_ANCHOR":
      return { ...state, anchorDate: action.date };
    case "SHIFT_PERIOD":
      return {
        ...state,
        anchorDate: shiftAnchor(state.anchorDate, state.zoom, action.dir),
      };
    case "JUMP_TODAY":
      return { ...state, anchorDate: anchorForToday(state.zoom) };
    case "SET_FILTERS":
      return { ...state, filters: { ...state.filters, ...action.patch } };
    case "SET_QUICK_CAPTURE":
      return { ...state, quickCaptureOpen: action.open };
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

function initialState(): AppState {
  // Seed view/project from the URL hash so deep links survive the initial
  // render (effect-based syncing races under StrictMode double-mount).
  const route =
    typeof window !== "undefined" ? parseHash(window.location.hash) : null;
  return {
    projects: PROJECTS,
    activities: ACTIVITIES,
    tasks: TASKS,
    notes: NOTES,
    reminders: REMINDERS,
    inbox: INBOX_ITEMS,
    view: route?.view ?? "timeline",
    selectedProjectId:
      route?.view === "projects" && route.projectId ? route.projectId : null,
    selectedActivityId: null,
    zoom: "day",
    anchorDate: anchorForToday("day"),
    filters: {
      projectIds: null,
      types: null,
      showPlanned: false,
      showCompleted: false,
    },
    quickCaptureOpen: false,
    navCollapsed: false,
    railCollapsed: false,
    searchQuery: "",
  };
}

const StateContext = React.createContext<AppState | null>(null);
const DispatchContext = React.createContext<React.Dispatch<AppAction> | null>(null);

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [state, dispatch] = React.useReducer(reducer, undefined, initialState);
  return (
    <StateContext.Provider value={state}>
      <DispatchContext.Provider value={dispatch}>{children}</DispatchContext.Provider>
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
