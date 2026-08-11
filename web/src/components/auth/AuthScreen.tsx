import * as React from "react";

import { ApiError } from "@/api/client";

/** Brand line used by every pre-app screen. */
export function BrandLine({ trailing }: { trailing?: React.ReactNode }) {
  return (
    <div className="select-none font-mono text-[0.85rem] font-semibold uppercase tracking-chrome text-gtc-text">
      donezo <span className="text-gtc-accent">//</span>
      {trailing != null && <span className="ml-2 font-normal text-gtc-muted">{trailing}</span>}
    </div>
  );
}

/** Full-viewport mono tick shown while the gate talks to the server. */
export function ConnectingScreen({ label = "connecting…" }: { label?: string }) {
  return (
    <div className="flex h-full items-center justify-center bg-gtc-page">
      <BrandLine trailing={label} />
    </div>
  );
}

/**
 * Links to the published policies, under the sign-in card.
 *
 * This is the only place they are reachable without an account, and that is
 * the point: the landing page is what somebody vetting the service sees —
 * a carrier reviewing an SMS program, or anyone deciding whether to hand
 * over a phone number — and a policy they cannot find is a policy that does
 * not count. It cost a campaign registration to learn that.
 *
 * Rendered only when this instance actually publishes them; the pages 404
 * otherwise, and a dead link on a privacy policy is worse than none.
 */
function PolicyLinks() {
  const [published, setPublished] = React.useState(false);

  React.useEffect(() => {
    let cancelled = false;
    void fetch("/privacy", { method: "HEAD" })
      .then((res) => !cancelled && setPublished(res.ok))
      .catch(() => !cancelled && setPublished(false));
    return () => {
      cancelled = true;
    };
  }, []);

  if (!published) return null;
  return (
    <p className="m-0 mt-4 text-center font-mono text-[0.68rem] uppercase tracking-label text-gtc-muted">
      <a
        href="/privacy"
        className="text-gtc-muted underline-offset-2 hover:text-gtc-text hover:underline focus-visible:text-gtc-text focus-visible:underline focus-visible:outline-none"
      >
        Privacy
      </a>
      <span className="px-1.5">·</span>
      <a
        href="/terms"
        className="text-gtc-muted underline-offset-2 hover:text-gtc-text hover:underline focus-visible:text-gtc-text focus-visible:underline focus-visible:outline-none"
      >
        Terms
      </a>
    </p>
  );
}

/** Centered narrow column wrapping the setup and login forms. */
export function AuthScreen({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex h-full items-center justify-center bg-gtc-page px-4 text-gtc-text">
      <div className="w-full max-w-sm">
        <div className="mb-5">
          <BrandLine trailing={title} />
        </div>
        <div className="rounded-gtc border border-gtc-line bg-gtc-panel px-5 py-5">
          {children}
        </div>
        <PolicyLinks />
      </div>
    </div>
  );
}

/** Calm, user-facing message for an auth request failure. */
export function authErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 429) return "too many attempts — wait a few minutes";
    if (err.status === 0) return "can't reach the server — try again in a moment";
    return err.message;
  }
  return err instanceof Error ? err.message : String(err);
}

/** Mono error line under a form; renders nothing without a message. */
export function AuthErrorLine({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p className="m-0 font-mono text-[0.74rem] text-gtc-danger" role="alert">
      ▸ {message}
    </p>
  );
}
