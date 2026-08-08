import * as React from "react";

import { fetchSpaceRevision, fetchSpaceState } from "@/api/client";
import { useAppDispatch, useSyncPending } from "@/state/AppStore";

/** How often to ask the server whether anything changed, while the tab is
 *  visible. Fast enough that a write from another machine or an agent feels
 *  immediate to someone watching; cheap enough to leave open all day, because
 *  the answer is a single integer the server holds in memory. */
const POLL_MS = 4_000;

/**
 * Keeps the store current as the server changes, without a manual reload.
 *
 * Until this existed the client only ever learned about changes it made
 * itself: a second tab, another machine, or an LLM writing over MCP were all
 * invisible until the page was reloaded. The MCP case is the one that made it
 * more than a papercut — donezo is meant to be usable by an agent as a
 * co-worker, and having to reload to see what your agent just did breaks the
 * loop that makes that worth anything.
 *
 * The shape is deliberately dull: poll a tiny revision endpoint, and only when
 * the number moves pay for a full state read. Nothing streams, so there is no
 * long-lived connection to keep alive through a reverse proxy, and no
 * reconnect logic to get wrong. Server-sent events would cut the latency to
 * well under a second; this is the version that earns that complexity first.
 *
 * Must be mounted inside the authenticated tree — an anonymous poll would just
 * 401 every four seconds.
 */
export function useSpaceFreshness(spaceId: string): void {
  const dispatch = useAppDispatch();
  const pending = useSyncPending();

  React.useEffect(() => {
    // Null until the first successful read. Seeding it from a poll rather
    // than assuming zero means a donezod that has been running a while does
    // not look like a change on first sight.
    let known: number | null = null;
    let stopped = false;
    let timer: number | undefined;
    // Guards against overlapping runs: a slow state read must not have a
    // second poll stacked behind it.
    let busy = false;

    const tick = async () => {
      if (stopped || busy) return;
      // A refresh taken while our own writes are still in the air can predate
      // them and would visibly roll the user's change back until the next
      // poll. Waiting costs one interval; the writes settle in milliseconds.
      if (pending.current > 0) return;
      busy = true;
      try {
        const revision = await fetchSpaceRevision(spaceId);
        if (stopped) return;
        if (known === null) {
          known = revision;
          return;
        }
        if (revision === known) return;
        // Re-check: the state read below is the expensive half, and a write
        // of our own may have started while the revision was in flight.
        if (pending.current > 0) return;
        const data = await fetchSpaceState(spaceId);
        if (stopped) return;
        known = revision;
        dispatch({ type: "REPLACE_STATE", data });
      } catch {
        // Offline, a dropped connection, or an expired session. Stay quiet
        // and try again next tick: this is a background refresh, and the
        // screen is still showing the last good data. Real failures the user
        // needs to know about surface through the sync-error banner, which
        // reports on things they actually did.
      } finally {
        busy = false;
      }
    };

    const start = () => {
      if (timer !== undefined) return;
      timer = window.setInterval(() => void tick(), POLL_MS);
      // Catch up immediately rather than waiting a full interval — coming
      // back to a backgrounded tab is exactly when it is most likely stale.
      void tick();
    };
    const stop = () => {
      if (timer === undefined) return;
      window.clearInterval(timer);
      timer = undefined;
    };

    // donezo is a thing people leave open in a background tab for days.
    // Polling one it cannot be read from is pure waste.
    const onVisibility = () => (document.hidden ? stop() : start());
    document.addEventListener("visibilitychange", onVisibility);
    if (!document.hidden) start();

    return () => {
      stopped = true;
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [spaceId, dispatch, pending]);
}
