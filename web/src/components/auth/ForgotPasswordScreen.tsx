import * as React from "react";
import { Button, Field, Input } from "@grewelltech/console";

import { forgotPassword } from "@/api/client";
import { looksLikeEmail } from "@/lib/email";
import { AuthScreen, AuthErrorLine, authErrorMessage } from "./AuthScreen";

/** Request a password-reset email. The server answers the same whether or not
 *  the address is on file, so this screen shows the same confirmation either
 *  way — it never reveals whether an account exists. */
export function ForgotPasswordScreen({ onBack }: { onBack: () => void }) {
  const [email, setEmail] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [sent, setSent] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const ready = looksLikeEmail(email) && !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      await forgotPassword(email.trim());
      setSent(true);
    } catch (err) {
      setError(authErrorMessage(err));
      setBusy(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      void submit();
    }
  };

  if (sent) {
    return (
      <AuthScreen title="check your email">
        <div className="space-y-4">
          <p className="m-0 text-[0.85rem] text-gtc-muted">
            If an account uses that address, a password-reset link is on its way. The link is good
            for one hour.
          </p>
          <Button variant="primary" className="w-full" onClick={onBack}>
            Back to sign in
          </Button>
        </div>
      </AuthScreen>
    );
  }

  return (
    <AuthScreen title="reset password">
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">
          Enter your account email and we&rsquo;ll send you a link to set a new password.
        </p>
        <Field label="Email" htmlFor="forgot-email">
          <Input
            id="forgot-email"
            type="email"
            autoFocus
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <AuthErrorLine message={error} />
        <Button variant="primary" className="w-full" disabled={!ready} onClick={() => void submit()}>
          {busy ? "Sending…" : "Send reset link"}
        </Button>
        <button
          type="button"
          onClick={onBack}
          className="block w-full rounded-gtc py-0.5 text-center font-mono text-[0.68rem] uppercase tracking-label text-gtc-muted outline-none transition-colors hover:text-gtc-text focus-visible:shadow-gtc-focus"
        >
          Back to sign in
        </button>
      </div>
    </AuthScreen>
  );
}
