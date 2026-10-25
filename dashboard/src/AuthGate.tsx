import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ApiError, fetchMe, login, logout, setupAdmin, type User } from "./api";
import { SessionContext, type Session } from "./session";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "setup" }
  | { kind: "login" }
  | { kind: "ready"; user: User };

// AuthGate shows the first-run setup or the login page until there is a
// session, then renders children.
export function AuthGate({ children }: { children: (session: Session) => ReactNode }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const expired = useCallback(() => setState({ kind: "login" }), []);

  useEffect(() => {
    const controller = new AbortController();
    fetchMe(controller.signal).then(
      (me) => setState(me.user ? { kind: "ready", user: me.user } : { kind: me.setup_required ? "setup" : "login" }),
      (err: unknown) => {
        if (controller.signal.aborted) return;
        setState({ kind: "error", message: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => controller.abort();
  }, []);

  switch (state.kind) {
    case "loading":
      return <AuthFrame><p>Loading…</p></AuthFrame>;
    case "error":
      return <AuthFrame><p role="alert">Could not reach the hub: {state.message}</p></AuthFrame>;
    case "setup":
      return <AuthFrame><SetupForm onDone={(user) => setState({ kind: "ready", user })} /></AuthFrame>;
    case "login":
      return <AuthFrame><LoginForm onDone={(user) => setState({ kind: "ready", user })} /></AuthFrame>;
    case "ready": {
      const session: Session = {
        user: state.user,
        signOut: async () => {
          try {
            await logout();
          } finally {
            setState({ kind: "login" });
          }
        },
        expired,
      };
      return <SessionContext value={session}>{children(session)}</SessionContext>;
    }
  }
}

// AuthFrame is the centred card around the setup and login pages.
export function AuthFrame({ children }: { children: ReactNode }) {
  return (
    <main className="auth">
      <header>
        <h1>Regulus</h1>
        <p>CodexCircle Server Manager</p>
      </header>
      {children}
    </main>
  );
}

function useSubmit(action: () => Promise<User>, onDone: (user: User) => void) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    action().then(onDone, (err: unknown) => {
      setError(err instanceof ApiError || err instanceof Error ? err.message : String(err));
      setBusy(false);
    });
  };
  return { error, busy, submit };
}

function LoginForm({ onDone }: { onDone: (user: User) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, busy, submit } = useSubmit(() => login(username, password), onDone);

  return (
    <form onSubmit={submit} aria-labelledby="login-heading">
      <h2 id="login-heading">Sign in</h2>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
      </label>
      <label>
        Password
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        Sign in
      </button>
      <p className="hint">Forgot your password? Ask an administrator for a reset link.</p>
    </form>
  );
}

function SetupForm({ onDone }: { onDone: (user: User) => void }) {
  const [token, setToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const { error, busy, submit } = useSubmit(() => setupAdmin(token.trim(), username, password), onDone);

  const onSubmit = (e: FormEvent) => {
    if (password !== confirm) {
      e.preventDefault();
      setMismatch(true);
      return;
    }
    setMismatch(false);
    submit(e);
  };

  return (
    <form onSubmit={onSubmit} aria-labelledby="setup-heading">
      <h2 id="setup-heading">Create the admin account</h2>
      <p>
        This hub has no accounts yet. Copy the one-time setup token from the hub log (the line that says
        &quot;no accounts yet&quot;).
      </p>
      <label>
        Setup token
        <input value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
      </label>
      <label>
        Password (at least 10 characters)
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          minLength={10}
          required
        />
      </label>
      <label>
        Repeat the password
        <input
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </label>
      {mismatch && <p role="alert">The passwords do not match.</p>}
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        Create the admin
      </button>
    </form>
  );
}
