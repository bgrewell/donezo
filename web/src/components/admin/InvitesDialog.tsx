import * as React from "react";
import { Check, Copy } from "lucide-react";
import { Button, Dialog, Select, cn } from "@grewelltech/console";

import {
  ApiError,
  createInvite,
  fetchInvites,
  revokeInvite,
  type CreatedInvite,
  type Invite,
} from "@/api/client";
import { useSession } from "@/components/auth/session";

const EXPIRY_CHOICES = [
  { days: 7, label: "Expires in 7 days" },
  { days: 30, label: "Expires in 30 days" },
  { days: 90, label: "Expires in 90 days" },
];

/** Calm per-status text color for the invite list. */
const STATUS_CLASS: Record<Invite["status"], string> = {
  active: "text-gtc-accent",
  used: "text-gtc-muted",
  expired: "text-gtc-muted",
  revoked: "text-gtc-danger-dim",
};

function statusWord(invite: Invite): string {
  if (invite.status === "used" && invite.usedBy) return `used-by-${invite.usedBy}`;
  return invite.status;
}

/**
 * Copies text without the async Clipboard API, which browsers expose
 * only in secure contexts (HTTPS or localhost). donezo Phase 1 is
 * served over plain HTTP, so an admin at http://<lan-ip> has no
 * navigator.clipboard — this legacy selection + execCommand path is
 * what keeps one-click copy working there. Throws when the copy is
 * refused so the caller can fall back to its manual-copy hint.
 */
function legacyCopy(text: string) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  // Keep the off-screen textarea from scrolling or flashing the page.
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  try {
    if (!document.execCommand("copy")) throw new Error("copy command refused");
  } finally {
    ta.remove();
  }
}

/**
 * Admin-only invite management: mint a code (shown exactly once — the
 * server stores only its hash), list every invite with its derived
 * status, and revoke active ones with a two-step inline confirm.
 * The caller hides the entry point from members; the server enforces
 * the admin requirement regardless.
 */
export function InvitesDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { sessionExpired } = useSession();

  const [invites, setInvites] = React.useState<Invite[] | null>(null);
  const [listError, setListError] = React.useState<string | null>(null);
  const [days, setDays] = React.useState(7);
  const [generating, setGenerating] = React.useState(false);
  const [generateError, setGenerateError] = React.useState<string | null>(null);
  // The one-time code well. Cleared on close — the code is unrecoverable
  // by design, so it must never linger into a later open.
  const [minted, setMinted] = React.useState<CreatedInvite | null>(null);
  const [copied, setCopied] = React.useState(false);
  const [confirmRevokeId, setConfirmRevokeId] = React.useState<string | null>(null);
  const [revokeError, setRevokeError] = React.useState<string | null>(null);

  const errorText = (err: unknown) => {
    if (err instanceof ApiError && err.status === 401) sessionExpired();
    if (err instanceof ApiError && err.status === 0) {
      return "can't reach the server — try again in a moment";
    }
    return err instanceof Error ? err.message : String(err);
  };

  const refresh = React.useCallback(async () => {
    try {
      setInvites(await fetchInvites());
      setListError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) sessionExpired();
      setListError(err instanceof Error ? err.message : String(err));
    }
  }, [sessionExpired]);

  // Fresh state per open: load the list, drop everything on close.
  React.useEffect(() => {
    if (open) {
      void refresh();
      return;
    }
    setInvites(null);
    setListError(null);
    setGenerateError(null);
    setMinted(null);
    setCopied(false);
    setConfirmRevokeId(null);
    setRevokeError(null);
    setGenerating(false);
  }, [open, refresh]);

  // "copied" tick reverts after a beat.
  React.useEffect(() => {
    if (!copied) return;
    const t = window.setTimeout(() => setCopied(false), 1800);
    return () => window.clearTimeout(t);
  }, [copied]);

  const generate = async () => {
    if (generating) return;
    setGenerating(true);
    setGenerateError(null);
    try {
      const invite = await createInvite(days);
      setMinted(invite);
      setCopied(false);
      await refresh();
    } catch (err) {
      setGenerateError(errorText(err));
    } finally {
      setGenerating(false);
    }
  };

  const copyCode = async () => {
    if (!minted) return;
    try {
      // navigator.clipboard exists only in secure contexts; over plain
      // HTTP on a LAN host the legacy path is the only one available.
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(minted.code);
      } else {
        legacyCopy(minted.code);
      }
      setCopied(true);
    } catch {
      // The modern API can also reject (e.g. permission policy) where
      // the legacy command still works — try it before giving up.
      try {
        legacyCopy(minted.code);
        setCopied(true);
      } catch {
        setGenerateError("couldn't copy — select the code and copy it manually");
      }
    }
  };

  const revoke = async (id: string) => {
    setRevokeError(null);
    try {
      await revokeInvite(id);
      setConfirmRevokeId(null);
      await refresh();
    } catch (err) {
      setRevokeError(errorText(err));
    }
  };

  const inviteRow = (invite: Invite) => {
    const confirming = confirmRevokeId === invite.id;
    return (
      <li
        key={invite.id}
        className="flex min-h-[2rem] flex-wrap items-center gap-x-3 gap-y-1 rounded-gtc px-2 py-1"
      >
        <span className="font-mono text-[0.78rem] text-gtc-text">{invite.codePrefix}…</span>
        <span
          className={cn(
            "font-mono text-[0.66rem] uppercase tracking-label",
            STATUS_CLASS[invite.status]
          )}
        >
          {statusWord(invite)}
        </span>
        <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
          {invite.status === "active" ? "expires" : "expiry"} {invite.expiresAt.slice(0, 10)}
        </span>
        <span className="flex-1" />
        {invite.status === "active" &&
          (confirming ? (
            <span className="flex items-center gap-1.5">
              <span className="font-mono text-[0.64rem] uppercase tracking-label text-gtc-danger">
                Revoke {invite.codePrefix}…?
              </span>
              <Button size="sm" variant="danger" noGlyph onClick={() => void revoke(invite.id)}>
                Confirm revoke
              </Button>
              <Button size="sm" variant="ghost" noGlyph onClick={() => setConfirmRevokeId(null)}>
                Keep
              </Button>
            </span>
          ) : (
            <Button
              size="sm"
              variant="ghost"
              noGlyph
              onClick={() => {
                setRevokeError(null);
                setConfirmRevokeId(invite.id);
              }}
            >
              Revoke
            </Button>
          ))}
      </li>
    );
  };

  return (
    <Dialog open={open} onClose={onClose} title="Invites" maxWidthClassName="max-w-xl">
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">
          Invite codes let someone create their own account, with their own spaces.
        </p>

        <div className="flex items-center gap-2">
          <Button variant="primary" disabled={generating} onClick={() => void generate()}>
            {generating ? "Generating…" : "Generate code"}
          </Button>
          <Select
            aria-label="Invite expiry"
            value={String(days)}
            onChange={(e) => setDays(Number(e.target.value))}
            className="!w-56 !py-[7px] !text-[0.78rem]"
          >
            {EXPIRY_CHOICES.map((c) => (
              <option key={c.days} value={c.days}>
                {c.label}
              </option>
            ))}
          </Select>
        </div>
        {generateError && (
          <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
            ▸ {generateError}
          </p>
        )}

        {minted && (
          <div className="space-y-2 rounded-gtc border border-gtc-line-hi bg-gtc-inset px-3 py-2.5">
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 select-all truncate font-mono text-[1rem] tracking-chrome text-gtc-text">
                {minted.code}
              </code>
              <Button size="sm" variant="ghost" noGlyph onClick={() => void copyCode()}>
                {copied ? (
                  <>
                    <Check className="h-3.5 w-3.5 text-gtc-success" aria-hidden />
                    Copied
                  </>
                ) : (
                  <>
                    <Copy className="h-3.5 w-3.5" aria-hidden />
                    Copy
                  </>
                )}
              </Button>
            </div>
            <p className="m-0 font-mono text-[0.66rem] uppercase tracking-label text-gtc-warn">
              shown only once — copy it now
            </p>
          </div>
        )}

        <div>
          <div className="pb-1.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            All invites
          </div>
          {listError ? (
            <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
              ▸ {listError}
            </p>
          ) : invites === null ? (
            <p className="m-0 font-mono text-[0.72rem] text-gtc-muted">loading…</p>
          ) : invites.length === 0 ? (
            <p className="m-0 text-[0.85rem] text-gtc-muted">No invites yet.</p>
          ) : (
            <ul className="m-0 max-h-64 list-none space-y-0.5 overflow-y-auto p-0">
              {invites.map(inviteRow)}
            </ul>
          )}
          {revokeError && (
            <p className="m-0 pt-1.5 font-mono text-[0.66rem] text-gtc-danger" role="alert">
              ▸ {revokeError}
            </p>
          )}
        </div>
      </div>
    </Dialog>
  );
}
