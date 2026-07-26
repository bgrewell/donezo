import * as React from "react";
import { Button, Field, Input } from "@grewelltech/console";

import { setup, type ApiUser } from "@/api/client";
import { AuthScreen, AuthErrorLine, authErrorMessage } from "./AuthScreen";

const MIN_PASSWORD_LENGTH = 10;

/** First-run screen: create the owner account. Shown while the server
 *  reports needsSetup; afterwards the login screen takes over. */
export function SetupScreen({ onDone }: { onDone: (user: ApiUser) => void }) {
  const [username, setUsername] = React.useState("");
  const [displayName, setDisplayName] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const passwordShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const ready = username.trim() !== "" && password.length >= MIN_PASSWORD_LENGTH && !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      const user = await setup(username.trim(), displayName.trim(), password);
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
    <AuthScreen title="first run">
      <div className="space-y-4">
        <p className="m-0 text-[0.85rem] text-gtc-muted">
          No account exists yet. Create the owner to get started.
        </p>
        <Field label="Username" htmlFor="setup-username">
          <Input
            id="setup-username"
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field label="Display name" htmlFor="setup-display" hint="Optional — defaults to the username.">
          <Input
            id="setup-display"
            autoComplete="name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field
          label="Password"
          htmlFor="setup-password"
          hint={passwordShort ? undefined : `At least ${MIN_PASSWORD_LENGTH} characters.`}
          error={passwordShort ? `at least ${MIN_PASSWORD_LENGTH} characters` : undefined}
        >
          <Input
            id="setup-password"
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <AuthErrorLine message={error} />
        <Button variant="primary" className="w-full" disabled={!ready} onClick={() => void submit()}>
          {busy ? "Creating…" : "Create account"}
        </Button>
      </div>
    </AuthScreen>
  );
}
