import * as React from "react";
import { Button, Field, Input } from "@grewelltech/console";

import { login, type ApiUser } from "@/api/client";
import { AuthScreen, AuthErrorLine, authErrorMessage } from "./AuthScreen";

/** Sign-in screen shown whenever the session cookie is absent or stale.
 *  `notice` adds a calm context line above the form (e.g. the session
 *  expired mid-use and the gate is overlaying re-auth). */
export function LoginScreen({
  notice,
  onDone,
}: {
  notice?: string;
  onDone: (user: ApiUser) => void;
}) {
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const ready = username.trim() !== "" && password !== "" && !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      const user = await login(username.trim(), password);
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
    <AuthScreen title="sign in">
      <div className="space-y-4">
        {notice && <p className="m-0 text-[0.8rem] text-gtc-muted">{notice}</p>}
        <Field label="Username" htmlFor="login-username">
          <Input
            id="login-username"
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <Field label="Password" htmlFor="login-password">
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={onKeyDown}
          />
        </Field>
        <AuthErrorLine message={error} />
        <Button variant="primary" className="w-full" disabled={!ready} onClick={() => void submit()}>
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </div>
    </AuthScreen>
  );
}
