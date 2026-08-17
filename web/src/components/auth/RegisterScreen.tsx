import * as React from "react";
import { Button, Field, Input } from "@grewelltech/console";

import { register, type ApiUser } from "@/api/client";
import { looksLikeEmail } from "@/lib/email";
import { AuthScreen, AuthErrorLine, authErrorMessage } from "./AuthScreen";

const MIN_PASSWORD_LENGTH = 10;

/** Invite-code registration: redeem a dz-XXXXX-XXXXX code into a member
 *  account. Reached from the login screen; success lands in the app
 *  exactly like login (the server creates the member's "main" space). */
export function RegisterScreen({
  onDone,
  onBack,
  initialCode,
}: {
  onDone: (user: ApiUser) => void;
  onBack: () => void;
  /** Prefill the code field, e.g. from an emailed join link. */
  initialCode?: string;
}) {
  const [code, setCode] = React.useState(initialCode ?? "");
  const [username, setUsername] = React.useState("");
  const [displayName, setDisplayName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const passwordShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const ready =
    code.trim() !== "" &&
    username.trim() !== "" &&
    looksLikeEmail(email) &&
    password.length >= MIN_PASSWORD_LENGTH &&
    !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      // The code is submitted as typed (trimmed) — the uppercase render
      // below is display-only, and the server matches codes
      // case-insensitively, so a hand-typed lowercase code still claims.
      const user = await register(code.trim(), username.trim(), displayName.trim(), password, email.trim());
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
    <AuthScreen title="join">
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">
          Enter the invite code you were given to create your account.
        </p>
        <Field label="Invite code" htmlFor="register-code">
          <Input
            id="register-code"
            autoFocus
            autoComplete="off"
            spellCheck={false}
            placeholder="dz-XXXXX-XXXXX"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            onKeyDown={onKeyDown}
            className="uppercase tracking-chrome placeholder:normal-case"
          />
        </Field>
        <Field label="Username" htmlFor="register-username">
          <Input
            id="register-username"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field
          label="Display name"
          htmlFor="register-display"
          hint="Optional — defaults to the username."
        >
          <Input
            id="register-display"
            autoComplete="name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field label="Email" htmlFor="register-email" hint="Used to reset your password if you forget it.">
          <Input
            id="register-email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field
          label="Password"
          htmlFor="register-password"
          hint={passwordShort ? undefined : `At least ${MIN_PASSWORD_LENGTH} characters.`}
          error={passwordShort ? `at least ${MIN_PASSWORD_LENGTH} characters` : undefined}
        >
          <Input
            id="register-password"
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <AuthErrorLine message={error} />
        <Button variant="primary" className="w-full" disabled={!ready} onClick={() => void submit()}>
          {busy ? "Creating account…" : "Create account"}
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
