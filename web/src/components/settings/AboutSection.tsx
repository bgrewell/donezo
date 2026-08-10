import * as React from "react";

import {
  fetchInstance,
  fetchLLMStatus,
  fetchNotifyStatus,
  type NotifyChannelStatus,
} from "@/api/client";

/** A label and its value, as a definition row. */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-b border-gtc-line py-1.5 last:border-b-0">
      <dt className="w-[150px] shrink-0 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
        {label}
      </dt>
      <dd className="m-0 min-w-0 flex-1 font-sans text-[0.82rem] text-gtc-text">{children}</dd>
    </div>
  );
}

/** The word for a switched-off capability. Says who can change it, because
 *  "off" with no explanation reads as a fault. */
function NotConfigured({ what }: { what: string }) {
  return (
    <span className="text-gtc-muted">
      Not configured — {what} is set up by whoever runs this instance.
    </span>
  );
}

/**
 * About: what this instance is running and what its operator has switched on.
 *
 * The version lives here rather than in the nav rail, where it spent a line
 * of permanent screen space on something you look at when something is
 * wrong and never otherwise.
 *
 * Everything on this page is read-only on purpose. Delivery channels and the
 * model connection are environment configuration, not app state — showing
 * them with an edit affordance would promise something the app cannot do.
 */
export function AboutSection() {
  const [version, setVersion] = React.useState<string | null | undefined>(undefined);
  const [channels, setChannels] = React.useState<NotifyChannelStatus[] | null>(null);
  const [model, setModel] = React.useState<{ enabled: boolean; label?: string } | null>(null);
  // Whether this instance publishes policy pages at all. Probed rather than
  // assumed: they are served only when an operator has been named.
  const [policies, setPolicies] = React.useState(false);

  React.useEffect(() => {
    let cancelled = false;
    // Each is best-effort and independent: a failure to read one of these
    // should leave the others on screen rather than blanking the page.
    void fetchInstance()
      .then((info) => !cancelled && setVersion(info.version ?? null))
      .catch(() => !cancelled && setVersion(null));
    void fetchNotifyStatus()
      .then((list) => !cancelled && setChannels(list))
      .catch(() => !cancelled && setChannels([]));
    void fetchLLMStatus()
      .then((status) => {
        if (cancelled) return;
        setModel({
          enabled: status.enabled,
          label: status.provider && status.model ? `${status.provider} / ${status.model}` : undefined,
        });
      })
      .catch(() => !cancelled && setModel({ enabled: false }));
    void fetch("/privacy", { method: "HEAD" })
      .then((res) => !cancelled && setPolicies(res.ok))
      .catch(() => !cancelled && setPolicies(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const email = channels?.find((c) => c.channel === "email");
  const sms = channels?.find((c) => c.channel === "sms");

  return (
    <section>
      <dl className="m-0">
        <Row label="Version">
          {version === undefined ? (
            <span className="font-mono text-[0.75rem] text-gtc-muted">loading…</span>
          ) : version ? (
            <span className="font-mono text-[0.78rem]">donezo {version}</span>
          ) : (
            // --hide-version is a deliberate choice, so this is not an error.
            <span className="text-gtc-muted">Not reported by this instance.</span>
          )}
        </Row>

        <Row label="Email delivery">
          {channels === null ? (
            <span className="font-mono text-[0.75rem] text-gtc-muted">loading…</span>
          ) : email?.configured ? (
            <span className="font-mono text-[0.75rem]">{email.provider}</span>
          ) : (
            <NotConfigured what="email" />
          )}
        </Row>

        <Row label="Text messages">
          {channels === null ? (
            <span className="font-mono text-[0.75rem] text-gtc-muted">loading…</span>
          ) : sms?.configured ? (
            <span className="font-mono text-[0.75rem]">{sms.provider}</span>
          ) : (
            <NotConfigured what="SMS" />
          )}
        </Row>

        <Row label="Language model">
          {model === null ? (
            <span className="font-mono text-[0.75rem] text-gtc-muted">loading…</span>
          ) : model.enabled ? (
            <span className="font-mono text-[0.75rem]">{model.label ?? "configured"}</span>
          ) : (
            <NotConfigured what="the model connection" />
          )}
        </Row>
      </dl>

      <p className="m-0 pt-3 text-[0.78rem] text-gtc-muted">
        A donezo with none of these configured is a fully supported donezo — every feature that
        does not need them works the same either way.
      </p>

      {/* Only rendered when this instance publishes them: the pages 404
          otherwise, and a dead link on a policy is worse than no link. */}
      {policies && (
        <p className="m-0 pt-2 text-[0.78rem] text-gtc-muted">
          <a
            href="/privacy"
            target="_blank"
            rel="noreferrer"
            className="text-gtc-accent underline-offset-2 hover:underline focus-visible:underline focus-visible:outline-none"
          >
            Privacy policy
          </a>
          {" · "}
          <a
            href="/terms"
            target="_blank"
            rel="noreferrer"
            className="text-gtc-accent underline-offset-2 hover:underline focus-visible:underline focus-visible:outline-none"
          >
            Terms and conditions
          </a>
        </p>
      )}
    </section>
  );
}
