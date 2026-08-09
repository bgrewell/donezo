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
 *  Best-effort in both directions, like the appearance sync: a failed read or
 *  write is dropped rather than surfaced. Nothing the user is doing depends on
 *  it, and the next load tries again.
 *
 *  Must be mounted inside the authenticated tree — an anonymous caller has no
 *  settings and every request would 401. */
export function useTimezoneSync(): void {
  React.useEffect(() => {
    const tz = browserTimezone();
    if (!tz) return;
    let cancelled = false;
    fetchUserSettings()
      .then((settings) => {
        if (cancelled) return;
        // Only write on a real change. An unconditional patch would mean a
        // settings write on every page load, for a value that changes about
        // as often as someone moves house.
        if (settings.timezone === tz) return;
        return saveUserSettings({ timezone: tz }).then(() => undefined);
      })
      .catch(() => {
        // Offline, or the session lapsed. The stored zone stays as it was,
        // which is the previous correct answer rather than a wrong one.
      });
    return () => {
      cancelled = true;
    };
  }, []);
}
