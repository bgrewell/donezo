import * as React from "react";
import { Button } from "@grewelltech/console";

import {
  ApiError,
  createSpace as apiCreateSpace,
  fetchAuthStatus,
  fetchMe,
  fetchSpaces,
  fetchSpaceState,
  logout as apiLogout,
  patchSpace,
  setSpaceArchived,
  type ApiUser,
  type SpaceData,
} from "@/api/client";
import type { ProjectColor, Space } from "@/domain/types";
import { ACTIVE_SPACE_STORAGE_KEY, SessionContext, type Session } from "./session";
import { AuthScreen, AuthErrorLine, ConnectingScreen, authErrorMessage } from "./AuthScreen";
import { LoginScreen } from "./LoginScreen";
import { RegisterScreen } from "./RegisterScreen";
import { SetupScreen } from "./SetupScreen";

type Phase = "connecting" | "setup" | "login" | "register" | "booting" | "ready" | "offline";

function readStoredSpaceId(): string | null {
  try {
    return window.localStorage.getItem(ACTIVE_SPACE_STORAGE_KEY);
  } catch {
    return null;
  }
}

/** An emailed invite links to #/join/<code>. The code rides in the fragment,
 *  which the browser never sends to the server, so it stays out of access logs
 *  and referrers. Returns the code to prefill the register form, or null. */
function readJoinCode(): string | null {
  const m = /^#\/join\/([A-Za-z0-9-]+)$/.exec(window.location.hash || "");
  return m ? m[1] : null;
}

/** Drop the join fragment from the address bar once it has been read into the
 *  form, so the single-use code does not linger in history or a later reload. */
function clearJoinHash() {
  try {
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
  } catch {
    // Best-effort — a stale fragment is harmless, just untidy.
  }
}

function storeSpaceId(id: string) {
  try {
    window.localStorage.setItem(ACTIVE_SPACE_STORAGE_KEY, id);
  } catch {
    // Private mode etc. — the choice just won't survive a reload.
  }
}

/** Last remembered space if it is still live, else first live space,
 *  else the remembered space (even archived), else first space at all.
 *  A remembered-but-archived space must not win over a live one — booting
 *  into an archive is only right when nothing live exists. */
function pickActiveSpace(spaces: Space[]): Space {
  const stored = readStoredSpaceId();
  const remembered = stored ? spaces.find((s) => s.id === stored) : undefined;
  if (remembered && !remembered.archivedAt) return remembered;
  return spaces.find((s) => !s.archivedAt) ?? remembered ?? spaces[0];
}

/**
 * Session gate around the whole app: decides between the setup, login,
 * and app screens, loads the active space's data before the store mounts,
 * and provides the Session context (spaces, switcher, logout) to the
 * shell. Children receive the space id and its server data — the caller
 * keys the store on the space id so switching remounts it fresh.
 */
export function AuthGate({
  children,
}: {
  children: (spaceId: string, data: SpaceData) => React.ReactNode;
}) {
  const [phase, setPhase] = React.useState<Phase>("connecting");
  const [offlineMessage, setOfflineMessage] = React.useState<string | null>(null);
  // An invite code carried in the URL fragment (#/join/<code>), read once at
  // mount so an emailed link lands the invitee on a prefilled register form.
  const [joinCode] = React.useState<string | null>(() => readJoinCode());
  const [user, setUser] = React.useState<ApiUser | null>(null);
  const [spaces, setSpaces] = React.useState<Space[]>([]);
  const [activeSpaceId, setActiveSpaceId] = React.useState<string | null>(null);
  const [data, setData] = React.useState<SpaceData | null>(null);
  // A mid-session 401 overlays sign-in WITHOUT unmounting the app, so
  // optimistic changes (and queued sync failures) survive re-auth.
  const [reauth, setReauth] = React.useState(false);

  const sessionExpired = React.useCallback(() => setReauth(true), []);

  // Wraps session operations so a mid-session 401 flips to the re-auth
  // overlay while still rejecting toward the caller's inline error slot.
  const withReauth = React.useCallback(
    async <T,>(op: Promise<T>): Promise<T> => {
      try {
        return await op;
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) setReauth(true);
        throw err;
      }
    },
    []
  );

  // Dedupes the zero-spaces auto-create: StrictMode's double-run (or any
  // re-entrant boot) must never create the initial space twice — spaces
  // can only be archived, never deleted.
  const firstSpaceRef = React.useRef<Promise<Space> | null>(null);

  // After auth: load spaces (creating a first one if the account has
  // none), pick the active space, and fetch its state before rendering.
  const boot = React.useCallback(async (authedUser: ApiUser) => {
    setUser(authedUser);
    setPhase("booting");
    try {
      let all = await fetchSpaces();
      if (all.length === 0) {
        firstSpaceRef.current ??= apiCreateSpace("main", "blue");
        all = [await firstSpaceRef.current];
      }
      const active = pickActiveSpace(all);
      const spaceData = await fetchSpaceState(active.id);
      storeSpaceId(active.id);
      setSpaces(all);
      setActiveSpaceId(active.id);
      setData(spaceData);
      setPhase("ready");
    } catch (err) {
      setOfflineMessage(authErrorMessage(err));
      setPhase("offline");
    }
  }, []);

  // Initial connect: status → setup / login / straight into the app.
  const connect = React.useCallback(async () => {
    setPhase("connecting");
    try {
      const status = await fetchAuthStatus();
      if (status.needsSetup) {
        // A fresh instance has no invites yet, so a join link cannot apply —
        // first-run setup wins.
        setPhase("setup");
      } else if (!status.authenticated) {
        if (joinCode) {
          // Consume the fragment as we open the prefilled register form.
          clearJoinHash();
          setPhase("register");
        } else {
          setPhase("login");
        }
      } else {
        await boot(await fetchMe());
      }
    } catch (err) {
      setOfflineMessage(authErrorMessage(err));
      setPhase("offline");
    }
  }, [boot, joinCode]);

  // Once-only: StrictMode double-mounts (mount → cleanup → remount) keep
  // the same refs, so this guard stops the whole status→me→spaces→state
  // chain from firing twice on every dev boot (a pre-await cancelled flag
  // cannot — the first run is already past it by cleanup time).
  const connectedRef = React.useRef(false);
  React.useEffect(() => {
    if (connectedRef.current) return;
    connectedRef.current = true;
    void connect();
  }, [connect]);

  const switchingRef = React.useRef(false);
  const switchSpace = React.useCallback(
    async (id: string) => {
      if (id === activeSpaceId || switchingRef.current) return;
      switchingRef.current = true;
      try {
        // The app stays mounted while the new space loads: a failed
        // switch leaves the current space — and any unsaved sync-failure
        // banner — completely untouched, and the caller surfaces the
        // rejection inline.
        const spaceData = await withReauth(fetchSpaceState(id));
        storeSpaceId(id);
        setActiveSpaceId(id);
        setData(spaceData);
      } finally {
        switchingRef.current = false;
      }
    },
    [activeSpaceId, withReauth]
  );

  const createSpace = React.useCallback(
    async (name: string, color: ProjectColor) => {
      const created = await withReauth(apiCreateSpace(name, color));
      setSpaces((prev) => [...prev, created]);
      // A new space is empty; render it without a round trip.
      storeSpaceId(created.id);
      setActiveSpaceId(created.id);
      setData({ projects: [], activities: [], tasks: [], notes: [], reminders: [], inbox: [] });
    },
    [withReauth]
  );

  const renameSpace = React.useCallback(
    async (id: string, name: string) => {
      const updated = await withReauth(patchSpace(id, { name }));
      setSpaces((prev) => prev.map((s) => (s.id === id ? updated : s)));
    },
    [withReauth]
  );

  const setArchived = React.useCallback(
    async (id: string, archived: boolean) => {
      const updated = await withReauth(setSpaceArchived(id, archived));
      const nextSpaces = spaces.map((s) => (s.id === id ? updated : s));
      setSpaces(nextSpaces);
      if (archived && id === activeSpaceId) {
        const fallback = nextSpaces.find((s) => !s.archivedAt);
        if (fallback) await switchSpace(fallback.id);
      }
    },
    [spaces, activeSpaceId, switchSpace, withReauth]
  );

  const logout = React.useCallback(() => {
    void apiLogout()
      .catch((err: unknown) => {
        // The cookie is expired either way; converge to logged out.
        console.error("donezo: logout", err);
      })
      .finally(() => {
        setUser(null);
        setSpaces([]);
        setActiveSpaceId(null);
        setData(null);
        setPhase("login");
      });
  }, []);

  if (phase === "connecting") return <ConnectingScreen />;
  if (phase === "booting") return <ConnectingScreen label="loading space…" />;
  if (phase === "offline") {
    return (
      <AuthScreen title="offline">
        <div className="space-y-4">
          <p className="m-0 text-[0.85rem] text-gtc-muted">
            The donezo server didn&rsquo;t answer.
          </p>
          <AuthErrorLine message={offlineMessage} />
          <Button variant="primary" className="w-full" onClick={() => void connect()}>
            Retry
          </Button>
        </div>
      </AuthScreen>
    );
  }
  if (phase === "setup") return <SetupScreen onDone={(u) => void boot(u)} />;
  if (phase === "login") {
    return (
      <LoginScreen onDone={(u) => void boot(u)} onRegister={() => setPhase("register")} />
    );
  }
  if (phase === "register") {
    // Success boots exactly like login — the server already created the
    // member's "main" space, so the zero-spaces auto-create never fires.
    return (
      <RegisterScreen
        onDone={(u) => void boot(u)}
        onBack={() => setPhase("login")}
        initialCode={joinCode ?? undefined}
      />
    );
  }

  if (!user || !activeSpaceId || !data) return <ConnectingScreen />;

  const session: Session = {
    user,
    spaces,
    activeSpaceId,
    switchSpace,
    createSpace,
    renameSpace,
    setArchived,
    sessionExpired,
    logout,
  };

  return (
    <SessionContext.Provider value={session}>
      {children(activeSpaceId, data)}
      {/* z-[60]: above every z-50 surface (dialogs, popovers, tour) —
          nothing may sit on top of re-auth. */}
      {reauth && (
        <div className="fixed inset-0 z-[60] bg-gtc-page">
          <LoginScreen
            notice="Your session expired — sign in to pick up where you left off. Unsaved changes are still here."
            onDone={(u) => {
              // Keep the mounted store: no boot, no refetch — the user
              // resumes exactly where the 401 interrupted them, and the
              // sync banner's Retry can now succeed.
              setUser(u);
              setReauth(false);
            }}
          />
        </div>
      )}
    </SessionContext.Provider>
  );
}
