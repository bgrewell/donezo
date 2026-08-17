import * as React from "react";
import { Button, Field, Input } from "@grewelltech/console";

import { resetPassword, type ApiUser } from "@/api/client";
import { AuthScreen, AuthErrorLine, authErrorMessage } from "./AuthScreen";

const MIN_PASSWORD_LENGTH = 10;

/** Set a new password from an emailed reset link. The token comes from the
 *  link (#/reset/<token>); success issues a session, so it lands in the app
 *  exactly like a fresh sign-in. */
export function ResetPasswordScreen({
  token,
  onDone,
  onBack,
}: {
  token: string;
  onDone: (user: ApiUser) => void;
  onBack: () => void;
}) {
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const passwordShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const ready = password.length >= MIN_PASSWORD_LENGTH && !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      const user = await resetPassword(token, password);
      onDone(user);
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

  return (
    <AuthScreen title="new password">
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">Choose a new password for your account.</p>
        <Field
          label="New password"
          htmlFor="reset-password"
          hint={passwordShort ? undefined : `At least ${MIN_PASSWORD_LENGTH} characters.`}
          error={passwordShort ? `at least ${MIN_PASSWORD_LENGTH} characters` : undefined}
        >
          <Input
            id="reset-password"
            type="password"
            autoFocus
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <AuthErrorLine message={error} />
        <Button variant="primary" className="w-full" disabled={!ready} onClick={() => void submit()}>
          {busy ? "Saving…" : "Set password and sign in"}
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
