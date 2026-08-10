import * as React from "react";
import { Button, cn } from "@grewelltech/console";

import {
  ApiError,
  fetchLLMStatus,
  saveUserSettings,
  type LLMPrompt,
} from "@/api/client";
import { useSession } from "@/components/auth/session";

/** Matches the server's per-prompt ceiling (internal/api/settings.go). Kept
 *  here so the count reads as a limit before the save is refused. */
const MAX_PROMPT_CHARS = 4000;

/**
 * Lets a user tune the wording of the model prompts in their own account.
 *
 * How far a rewrite should go is personal, so the instruction is editable —
 * but only the part that is a matter of taste. Each prompt has a fixed core
 * appended to whatever is saved here, shown read-only above the editor: it
 * holds the guarantees that stop a rewrite being harmful rather than merely
 * not to someone's liking, and a constraint the user cannot see would be
 * worse than one they can.
 *
 * Empty saves as "no override", which is how someone returns to the shipped
 * wording. Everything is per-user; the model connection itself is
 * instance-wide and configured by the operator.
 */
export function PolishPromptSection() {
  const { sessionExpired } = useSession();

  const [prompts, setPrompts] = React.useState<LLMPrompt[] | null>(null);
  const [enabled, setEnabled] = React.useState(true);
  const [draft, setDraft] = React.useState("");
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [saveError, setSaveError] = React.useState<string | null>(null);
  const [saving, setSaving] = React.useState(false);
  const [saved, setSaved] = React.useState(false);

  // One prompt exists today. Editing the first keeps this honest without
  // pretending to a selector that would have nothing to select between.
  const prompt = prompts?.[0] ?? null;

  const errorText = (err: unknown) => {
    if (err instanceof ApiError && err.status === 401) sessionExpired();
    if (err instanceof ApiError && err.status === 0) {
      return "can't reach the server — try again in a moment";
    }
    return err instanceof Error ? err.message : String(err);
  };

  // Mounted only while on screen, so this is the whole lifecycle.
  React.useEffect(() => {
    let cancelled = false;
    fetchLLMStatus()
      .then((status) => {
        if (cancelled) return;
        setPrompts(status.prompts);
        setEnabled(status.enabled);
        setDraft(status.prompts[0]?.body ?? "");
        setLoadError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setLoadError(errorText(err));
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The "saved" tick reverts after a beat.
  React.useEffect(() => {
    if (!saved) return;
    const t = window.setTimeout(() => setSaved(false), 1800);
    return () => window.clearTimeout(t);
  }, [saved]);

  const trimmed = draft.trim();
  const isDefault = prompt !== null && trimmed === prompt.default.trim();
  const dirty = prompt !== null && trimmed !== prompt.body.trim();
  const tooLong = trimmed.length > MAX_PROMPT_CHARS;

  const save = async (value: string) => {
    if (!prompt || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      // Saving the shipped wording verbatim stores nothing: it would only
      // pin this account to today's default and stop it following an
      // improved one in a later release.
      const body = value.trim() === prompt.default.trim() ? "" : value;
      await saveUserSettings({ prompts: { [prompt.id]: body } });
      const status = await fetchLLMStatus();
      setPrompts(status.prompts);
      setDraft(status.prompts[0]?.body ?? "");
      setSaved(true);
    } catch (err) {
      setSaveError(errorText(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section>
      <div className="space-y-4">
        <p className="font-sans text-[0.85rem] leading-relaxed text-gtc-text">
          This is the instruction sent with a capture when you ask the model to
          clean it up. Change how far it goes — lighter touch, heavier rewrite,
          a house style it should keep to.
        </p>

        {!enabled && (
          <p className="rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-2 font-sans text-[0.8rem] text-gtc-muted">
            No model is configured on this instance, so nothing is using this
            yet. Your wording is saved and will apply once one is.
          </p>
        )}

        {loadError && (
          <p className="font-sans text-[0.8rem] text-gtc-warn">
            Couldn&rsquo;t load the prompt: {loadError}
          </p>
        )}

        {prompt && (
          <>
            <div className="space-y-1.5">
              <label
                htmlFor="dz-polish-prompt"
                className="block font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted"
              >
                Your wording
                {prompt.customized && (
                  <span className="ml-2 normal-case text-gtc-accent">customized</span>
                )}
              </label>
              <textarea
                id="dz-polish-prompt"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                rows={9}
                spellCheck
                className={cn(
                  "w-full resize-y rounded-gtc border bg-gtc-inset px-3 py-2",
                  "font-sans text-[0.85rem] leading-relaxed text-gtc-text",
                  "outline-none transition-colors focus-visible:border-gtc-accent focus-visible:shadow-gtc-focus",
                  tooLong ? "border-gtc-warn-dim" : "border-gtc-line"
                )}
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span
                  className={cn(
                    "font-mono text-[0.64rem] uppercase tracking-label",
                    tooLong ? "text-gtc-warn" : "text-gtc-muted"
                  )}
                >
                  {trimmed.length} / {MAX_PROMPT_CHARS}
                </span>
                <span className="font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted">
                  {isDefault ? "matches the default" : "differs from the default"}
                </span>
              </div>
            </div>

            <div className="space-y-1.5">
              <span className="block font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                Always appended
              </span>
              <p className="rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-2 font-sans text-[0.8rem] leading-relaxed text-gtc-muted">
                {prompt.core}
              </p>
              <p className="font-sans text-[0.78rem] leading-relaxed text-gtc-muted">
                This part isn&rsquo;t editable. A capture is text you typed, not
                a request to the model, and the reply is written back over your
                own words — so those two rules hold whatever else the prompt
                says.
              </p>
            </div>

            {saveError && (
              <p className="font-sans text-[0.8rem] text-gtc-warn">{saveError}</p>
            )}

            <div className="flex flex-wrap items-center justify-end gap-3">
              <Button
                size="sm"
                variant="ghost"
                noGlyph
                disabled={saving || isDefault}
                onClick={() => {
                  setDraft(prompt.default);
                  void save(prompt.default);
                }}
                className="whitespace-nowrap"
              >
                Reset to default
              </Button>
              <Button
                size="sm"
                variant="primary"
                disabled={saving || !dirty || tooLong}
                onClick={() => void save(draft)}
                className="whitespace-nowrap"
              >
                {saving ? "Saving…" : saved ? "Saved" : "Save"}
              </Button>
            </div>
          </>
        )}
      </div>
    </section>
  );
}
