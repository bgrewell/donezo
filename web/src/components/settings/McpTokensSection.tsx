import * as React from "react";
import { Check, Copy } from "lucide-react";
import { Button, Input, Select, cn } from "@grewelltech/console";

import {
  ApiError,
  createApiToken,
  fetchApiTokens,
  revokeApiToken,
  type ApiToken,
  type ApiTokenScope,
  type CreatedApiToken,
} from "@/api/client";
import { useSession } from "@/components/auth/session";
import { relativeFromInstant } from "@/lib/time";

/** User-facing MCP scope labels for the generate Select. */
const SCOPE_CHOICES: { value: ApiTokenScope; label: string }[] = [
  { value: "read_write", label: "Read & write" },
  { value: "read_only", label: "Read only" },
];

/** Compact scope word for the list rows. */
const SCOPE_WORD: Record<ApiTokenScope, string> = {
  read_only: "read-only",
  read_write: "read-write",
};

/** Which copy button most recently fired, so only its tick shows. */
type CopyTarget = "token" | "snippet";

/**
 * Copies text without the async Clipboard API, which browsers expose
 * only in secure contexts (HTTPS or localhost). donezo instances may be
 * served over plain HTTP on a LAN, where navigator.clipboard is absent —
 * this legacy selection + execCommand path keeps one-click copy working.
 * Throws when the copy is refused so the caller can fall back to its
 * manual-copy hint.
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

/** The Claude Code one-liner that registers this instance as an MCP server,
 *  with the real token and this window's origin baked in.
 *
 *  `-s user` is deliberate. The flag defaults to `local`, which registers the
 *  server for one project directory only — but a donezo token is per user and
 *  reaches every space that user owns, so the natural match is the user scope:
 *  paste it once and donezo is there in every project. */
function claudeCodeSnippet(token: string): string {
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  return `claude mcp add -s user --transport http donezo ${origin}/mcp --header "Authorization: Bearer ${token}"`;
}

/**
 * Per-user API token management for connecting an LLM over MCP: mint a
 * token (shown exactly once — the server stores only its hash), see a
 * ready-to-paste Claude Code setup snippet, list every token with its
 * scope and usage, and revoke active ones with a two-step inline confirm.
 * Every user manages their own tokens; the server scopes the endpoints to
 * the caller regardless of what the UI shows.
 */
export function McpTokensSection() {
  const { sessionExpired } = useSession();

  const [tokens, setTokens] = React.useState<ApiToken[] | null>(null);
  const [listError, setListError] = React.useState<string | null>(null);
  const [name, setName] = React.useState("");
  const [scope, setScope] = React.useState<ApiTokenScope>("read_write");
  const [generating, setGenerating] = React.useState(false);
  const [generateError, setGenerateError] = React.useState<string | null>(null);
  // The one-time secret well. Cleared on close — the token is unrecoverable
  // by design, so it must never linger into a later open.
  const [minted, setMinted] = React.useState<CreatedApiToken | null>(null);
  const [copied, setCopied] = React.useState<CopyTarget | null>(null);
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
      setTokens(await fetchApiTokens());
      setListError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) sessionExpired();
      setListError(err instanceof Error ? err.message : String(err));
    }
  }, [sessionExpired]);

  // The section is mounted only while it is the one on screen, so loading
  // on mount is the whole lifecycle — there is no closed state to reset.
  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  // "copied" tick reverts after a beat.
  React.useEffect(() => {
    if (!copied) return;
    const t = window.setTimeout(() => setCopied(null), 1800);
    return () => window.clearTimeout(t);
  }, [copied]);

  const generate = async () => {
    if (generating || !name.trim()) return;
    setGenerating(true);
    setGenerateError(null);
    try {
      const token = await createApiToken(name.trim(), scope);
      setMinted(token);
      setCopied(null);
      setName("");
      await refresh();
    } catch (err) {
      setGenerateError(errorText(err));
    } finally {
      setGenerating(false);
    }
  };

  const copy = async (text: string, target: CopyTarget) => {
    try {
      // navigator.clipboard exists only in secure contexts; over plain HTTP
      // on a LAN host the legacy path is the only one available.
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      } else {
        legacyCopy(text);
      }
      setCopied(target);
    } catch {
      // The modern API can also reject (e.g. permission policy) where the
      // legacy command still works — try it before giving up.
      try {
        legacyCopy(text);
        setCopied(target);
      } catch {
        setGenerateError("couldn't copy — select the text and copy it manually");
      }
    }
  };

  const revoke = async (id: string) => {
    setRevokeError(null);
    try {
      await revokeApiToken(id);
      setConfirmRevokeId(null);
      await refresh();
    } catch (err) {
      setRevokeError(errorText(err));
    }
  };

  const tokenRow = (token: ApiToken) => {
    const revoked = Boolean(token.revokedAt);
    const confirming = confirmRevokeId === token.id;
    return (
      <li
        key={token.id}
        className="flex min-h-[2rem] flex-wrap items-center gap-x-3 gap-y-1 rounded-gtc px-2 py-1"
      >
        <span
          className={cn(
            "font-sans text-[0.82rem]",
            revoked ? "text-gtc-muted" : "text-gtc-text"
          )}
        >
          {token.name}
        </span>
        <span
          className={cn(
            "font-mono text-[0.78rem]",
            revoked ? "text-gtc-muted" : "text-gtc-text"
          )}
        >
          {token.tokenPrefix}…
        </span>
        <span
          className={cn(
            "font-mono text-[0.66rem] uppercase tracking-label",
            revoked ? "text-gtc-muted" : "text-gtc-accent"
          )}
        >
          {SCOPE_WORD[token.scope]}
        </span>
        <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
          created {relativeFromInstant(token.createdAt)}
        </span>
        <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
          {token.lastUsedAt ? `used ${relativeFromInstant(token.lastUsedAt)}` : "never"}
        </span>
        <span className="flex-1" />
        {revoked ? (
          <span className="font-mono text-[0.66rem] uppercase tracking-label text-gtc-danger-dim">
            revoked
          </span>
        ) : confirming ? (
          <span className="flex items-center gap-1.5">
            <span className="font-mono text-[0.64rem] uppercase tracking-label text-gtc-danger">
              Revoke {token.tokenPrefix}…?
            </span>
            <Button size="sm" variant="danger" noGlyph onClick={() => void revoke(token.id)}>
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
              setConfirmRevokeId(token.id);
            }}
          >
            Revoke
          </Button>
        )}
      </li>
    );
  };

  return (
    <section>
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="Token name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void generate();
            }}
            placeholder="claude code on laptop"
            className="!w-64 !py-[7px] !font-sans !text-[0.8rem]"
          />
          <Select
            aria-label="Token scope"
            value={scope}
            onChange={(e) => setScope(e.target.value as ApiTokenScope)}
            className="!w-44 !py-[7px] !text-[0.78rem]"
          >
            {SCOPE_CHOICES.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </Select>
          <Button
            variant="primary"
            disabled={generating || !name.trim()}
            onClick={() => void generate()}
          >
            {generating ? "Generating…" : "Generate token"}
          </Button>
        </div>
        {generateError && (
          <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
            ▸ {generateError}
          </p>
        )}

        {minted && (
          <div className="space-y-3 rounded-gtc border border-gtc-line-hi bg-gtc-inset px-3 py-2.5">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <code className="min-w-0 flex-1 select-all truncate font-mono text-[0.95rem] tracking-chrome text-gtc-text">
                  {minted.token}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  noGlyph
                  onClick={() => void copy(minted.token, "token")}
                >
                  {copied === "token" ? (
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

            <div className="space-y-1.5">
              <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                Claude Code setup
              </div>
              <div className="flex items-start gap-2 rounded-gtc border border-gtc-line bg-gtc-panel px-2.5 py-2">
                <code className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[0.72rem] leading-relaxed text-gtc-text">
                  {claudeCodeSnippet(minted.token)}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  noGlyph
                  onClick={() => void copy(claudeCodeSnippet(minted.token), "snippet")}
                >
                  {copied === "snippet" ? (
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
              <p className="m-0 text-[0.72rem] text-gtc-muted">
                Other clients →{" "}
                <a
                  href="https://github.com/bgrewell/donezo/blob/main/docs/mcp.md"
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono text-gtc-accent underline-offset-2 hover:underline focus-visible:underline focus-visible:outline-none"
                >
                  docs/mcp.md
                </a>
              </p>
            </div>
          </div>
        )}

        <div>
          <div className="pb-1.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
            Your tokens
          </div>
          {listError ? (
            <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
              ▸ {listError}
            </p>
          ) : tokens === null ? (
            <p className="m-0 font-mono text-[0.72rem] text-gtc-muted">loading…</p>
          ) : tokens.length === 0 ? (
            <p className="m-0 text-[0.85rem] text-gtc-muted">
              No tokens yet. Generate one to connect Claude or another MCP client.
            </p>
          ) : (
            <ul className="m-0 max-h-64 list-none space-y-0.5 overflow-y-auto p-0">
              {tokens.map(tokenRow)}
            </ul>
          )}
          {revokeError && (
            <p className="m-0 pt-1.5 font-mono text-[0.66rem] text-gtc-danger" role="alert">
              ▸ {revokeError}
            </p>
          )}
        </div>
      </div>
    </section>
  );
}
