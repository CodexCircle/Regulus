import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import {
  ApiError,
  fetchMe,
  isPending,
  login,
  loginTwoFactor,
  logout,
  setupAdmin,
  TWO_FACTOR_REQUIRED_EVENT,
  type LoginResult,
  type Me,
  type User,
} from "./api";
import { SessionContext, type Session } from "./session";
import { TwoFactor } from "./TwoFactor";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "setup" }
  | { kind: "login" }
  // mustEnroll: the hub requires two-factor authentication and the user has
  // not set it up, so nothing else works yet. recoveryLeft is set when the
  // user signed in with a recovery code.
  | { kind: "ready"; user: User; mustEnroll: boolean; recoveryLeft?: number };

const stateOf = (me: Me): State =>
  me.user
    ? { kind: "ready", user: me.user, mustEnroll: me.two_factor_setup_required === true }
    : { kind: me.setup_required ? "setup" : "login" };

// AuthGate shows the first-run setup or the login page until there is a
// session, then renders children.
export function AuthGate({ children }: { children: (session: Session) => ReactNode }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const expired = useCallback(() => setState({ kind: "login" }), []);

  // loadMe asks who is signed in, for the start and after a login (to learn
  // whether the user still has to set up two-factor authentication).
  const loadMe = useCallback((signal?: AbortSignal, keep?: Pick<Extract<State, { kind: "ready" }>, "recoveryLeft">) => {
    fetchMe(signal).then(
      (me) => {
        const next = stateOf(me);
        setState(next.kind === "ready" && keep ? { ...next, ...keep } : next);
      },
      (err: unknown) => {
        if (signal?.aborted) return;
        setState({ kind: "error", message: err instanceof Error ? err.message : String(err) });
      },
    );
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    loadMe(controller.signal);
    return () => controller.abort();
  }, [loadMe]);

  // The hub can start requiring two-factor while someone is signed in.
  useEffect(() => {
    const onRequired = () => loadMe();
    window.addEventListener(TWO_FACTOR_REQUIRED_EVENT, onRequired);
    return () => window.removeEventListener(TWO_FACTOR_REQUIRED_EVENT, onRequired);
  }, [loadMe]);

  const signedIn = (result: LoginResult) =>
    loadMe(undefined, result.recovery_code_used ? { recoveryLeft: result.recovery_codes_left ?? 0 } : undefined);

  switch (state.kind) {
    case "loading":
      return <AuthFrame><p>Loading…</p></AuthFrame>;
    case "error":
      return <AuthFrame><p role="alert">Could not reach the hub: {state.message}</p></AuthFrame>;
    case "setup":
      return <AuthFrame><SetupForm onDone={signedIn} /></AuthFrame>;
    case "login":
      return <AuthFrame><LoginForm onDone={signedIn} /></AuthFrame>;
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
      if (state.mustEnroll) {
        return (
          <SessionContext value={session}>
            <AuthFrame>
              <h2>Set up two-factor authentication</h2>
              <p>This hub requires two-factor authentication. You can use Regulus once it is on.</p>
              <TwoFactor onChange={() => loadMe()} />
              <p>
                <button type="button" onClick={() => void session.signOut()}>
                  Sign out
                </button>
              </p>
            </AuthFrame>
          </SessionContext>
        );
      }
      return (
        <SessionContext value={session}>
          {state.recoveryLeft !== undefined && (
            <RecoveryNotice
              left={state.recoveryLeft}
              onClose={() => setState({ kind: "ready", user: state.user, mustEnroll: false })}
            />
          )}
          {children(session)}
        </SessionContext>
      );
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

function useSubmit<T>(action: () => Promise<T>, onDone: (result: T) => void) {
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

function LoginForm({ onDone }: { onDone: (result: LoginResult) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  // Set once the password was right and the account asks for a code.
  const [pending, setPending] = useState<string | null>(null);
  const { error, busy, submit } = useSubmit(
    () => login(username, password),
    (result) => {
      if (isPending(result)) setPending(result.pending_token);
      else onDone(result);
    },
  );

  if (pending) {
    return <CodeForm pending={pending} onDone={onDone} onBack={() => setPending(null)} />;
  }
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

// CodeForm is the second step of a login: the code of the authenticator app,
// or a recovery code.
function CodeForm({ pending, onDone, onBack }: { pending: string; onDone: (r: LoginResult) => void; onBack: () => void }) {
  const [code, setCode] = useState("");
  const { error, busy, submit } = useSubmit(() => loginTwoFactor(pending, code), onDone);
  return (
    <form onSubmit={submit} aria-labelledby="code-heading">
      <h2 id="code-heading">Two-factor authentication</h2>
      <label>
        Authentication code
        <input
          value={code}
          onChange={(e) => setCode(e.target.value)}
          autoComplete="one-time-code"
          inputMode="text"
          autoFocus
          required
        />
      </label>
      <p className="hint">Enter the 6-digit code from your authenticator app, or one of your recovery codes.</p>
      {error && <p role="alert">{error}</p>}
      <p className="row-actions">
        <button type="submit" disabled={busy}>
          Verify
        </button>
        <button type="button" onClick={onBack}>
          Back
        </button>
      </p>
    </form>
  );
}

// RecoveryNotice warns that a recovery code was used and how many are left.
function RecoveryNotice({ left, onClose }: { left: number; onClose: () => void }) {
  return (
    <p role="status" className="note warn">
      You signed in with a recovery code; {left} {left === 1 ? "is" : "are"} left. If this was not you, change your password. Make new codes on the
      Account page. <button type="button" onClick={onClose}>Dismiss</button>
    </p>
  );
}

function SetupForm({ onDone }: { onDone: (result: LoginResult) => void }) {
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
