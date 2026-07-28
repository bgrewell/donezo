import { Button } from "@grewelltech/console";

import { useSyncErrors } from "@/state/AppStore";

/**
 * Persistent calm banner at the top of the workspace when a mutation
 * failed to reach the server. The optimistic local change stays applied;
 * Retry re-fires the same request, Dismiss drops the entry. One failure
 * shows at a time (oldest first) with a count of any queued behind it.
 */
export function SyncErrorBanner() {
  const { failures, retry, dismiss } = useSyncErrors();
  if (failures.length === 0) return null;
  const failure = failures[0];

  return (
    <div
      role="alert"
      className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-gtc-line bg-gtc-panel px-3 py-2"
    >
      <span className="font-mono text-[0.68rem] uppercase tracking-label text-gtc-warn">
        A change didn&rsquo;t save
      </span>
      <span className="min-w-0 flex-1 truncate text-[0.8rem] text-gtc-muted">
        the server said: {failure.message}
        {failures.length > 1 && (
          <span className="ml-2 font-mono text-[0.66rem] uppercase tracking-label">
            +{failures.length - 1} more
          </span>
        )}
      </span>
      <div className="flex shrink-0 items-center gap-1.5">
        <Button size="sm" noGlyph onClick={() => retry(failure.id)}>
          Retry
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={() => dismiss(failure.id)}>
          Dismiss
        </Button>
      </div>
    </div>
  );
}
