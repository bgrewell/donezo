import * as React from "react";

import { fetchLLMStatus, type LLMStatus } from "@/api/client";

// The instance's model configuration cannot change while the tab is open —
// it comes from the server's environment — so it is fetched once per page
// load and shared, rather than re-requested by every component that wants
// to know whether to offer a model-backed affordance.

const DISABLED: LLMStatus = { enabled: false, prompts: [] };

let cached: LLMStatus | null = null;
let inFlight: Promise<LLMStatus> | null = null;

/** Fetches the status once, sharing one request across all callers. */
function load(): Promise<LLMStatus> {
  if (cached) return Promise.resolve(cached);
  if (!inFlight) {
    inFlight = fetchLLMStatus()
      .then((status) => {
        cached = status;
        return status;
      })
      .catch(() => {
        // Could not ask: treat model features as unavailable. Hiding an
        // affordance is the right failure — offering one that cannot work
        // is worse than not offering it.
        cached = DISABLED;
        return DISABLED;
      })
      .finally(() => {
        inFlight = null;
      });
  }
  return inFlight;
}

/** Reports what this instance can do with a language model. Starts
 *  disabled and settles once the status arrives, so a component never
 *  flashes an affordance it may have to withdraw. */
export function useLLMStatus(): LLMStatus {
  const [status, setStatus] = React.useState<LLMStatus>(cached ?? DISABLED);

  React.useEffect(() => {
    if (cached) return;
    let cancelled = false;
    void load().then((next) => {
      if (!cancelled) setStatus(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return status;
}

/** Clears the cached status. Exists for tests; production has no reason to
 *  refetch, since the configuration is fixed for the server's lifetime. */
export function resetLLMStatusCache(): void {
  cached = null;
  inFlight = null;
}
