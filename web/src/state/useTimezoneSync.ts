import * as React from "react";

import { fetchUserSettings, saveUserSettings } from "@/api/client";

// The server has to decide what day it is for writes that arrive without a
// browser attached — an agent over MCP, most of all. Left to itself it can
// only guess, and a guess is wrong for however many hours the user is offset
// from it: entries logged in the evening land on tomorrow.
//
// The browser is the one place that knows for certain, so it reports the zone
// rather than making anyone find a setting. Reporting on every load means it
// follows the user to a new machine, and across a move or a trip.

/** The browser's IANA zone, or null where it cannot be determined. */
function browserTimezone(): string | null {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    // "Local" would be stored and then mean something different on the
    // server than it did here; the API refuses it, so do not send it.
    return tz && tz !== "Local" ? tz : null;
  } catch {
    return null;
  }
}

/** Reports this browser's timezone to the server when it differs from what is
 *  stored.
 *
 *  Re-checked when the tab becomes visible again, not only at mount. donezo is
 *  a thing people leave open in a background tab for days (the same premise
 *  useSpaceFreshness is built on), so a laptop that travels would otherwise
 *  keep the server on the zone it was in when the tab was opened — the web UI
 *  would move to the new day and MCP would not, which is issue #39 running
 *  backwards.
 *
 *  Best-effort in both directions, like the appearance sync: a failed read or
 *  write is dropped rather than surfaced. Nothing the user is doing depends on
 *  it, and the next check tries again.
 *
 *  Must be mounted inside the authenticated tree — an anonymous caller has no
 *  settings and every request would 401. */
export function useTimezoneSync(): void {
  React.useEffect(() => {
    let cancelled = false;
    // What the server is believed to hold. Remembering it means a tab that
    // wakes up fifty times only reads settings on the first one.
    let remote: string | null = null;

    const check = () => {
      const tz = browserTimezone();
      if (!tz || cancelled || tz === remote) return;
      fetchUserSettings()
        .then((settings) => {
          if (cancelled) return;
          remote = settings.timezone ?? null;
          // Only write on a real change. An unconditional patch would mean a
          // settings write on every page load, for a value that changes about
          // as often as someone moves house.
          if (remote === tz) return;
          return saveUserSettings({ timezone: tz }).then(() => {
            if (!cancelled) remote = tz;
          });
        })
        .catch(() => {
          // Offline, or the session lapsed. The stored zone stays as it was,
          // which is the previous correct answer rather than a wrong one, and
          // remote stays unset so the next wake tries again.
        });
    };

    check();
    const onVisible = () => {
      if (document.visibilityState === "visible") check();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);
}
