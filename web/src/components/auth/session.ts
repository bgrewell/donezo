import * as React from "react";

import type { ApiUser } from "@/api/client";
import type { ProjectColor, Space } from "@/domain/types";

/** localStorage key remembering the last active space id. */
export const ACTIVE_SPACE_STORAGE_KEY = "donezo.activeSpace";

/**
 * Authenticated-session surface, provided by AuthGate above the store so
 * the shell (space switcher, capture space chips, avatar menu) can reach
 * the user, the space list, and the space lifecycle without the store
 * knowing about any of it.
 */
export interface Session {
  user: ApiUser;
  /** All owned spaces, archived included, in registry order. */
  spaces: Space[];
  activeSpaceId: string;
  /** Switch the app to another owned space (refetches its state and
   *  remounts the store). Rejects on failure with the current space —
   *  and any unsaved sync failures — left untouched, so callers can
   *  surface the error inline. No-op for the active space. */
  switchSpace: (id: string) => Promise<void>;
  /** Create a space and switch to it. */
  createSpace: (name: string, color: ProjectColor) => Promise<void>;
  renameSpace: (id: string, name: string) => Promise<void>;
  /** Archive/unarchive; archiving the active space hops to another live one. */
  setArchived: (id: string, archived: boolean) => Promise<void>;
  /** Report a mid-session 401: the gate keeps the app (and its optimistic
   *  state) mounted and overlays sign-in, so queued sync failures can be
   *  retried successfully after re-auth. */
  sessionExpired: () => void;
  logout: () => void;
}

export const SessionContext = React.createContext<Session | null>(null);

export function useSession(): Session {
  const ctx = React.useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used within AuthGate");
  return ctx;
}
