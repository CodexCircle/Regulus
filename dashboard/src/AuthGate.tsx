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
import { useT } from "./i18n";
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
  const t = useT();
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
      return <AuthFrame><p>{t("auth.loading")}</p></AuthFrame>;
    case "error":
      return <AuthFrame><p role="alert">{t("auth.unreachable", { message: state.message })}</p></AuthFrame>;
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
              <h2>{t("auth.enroll.title")}</h2>
              <p>{t("auth.enroll.intro")}</p>
              <TwoFactor onChange={() => loadMe()} />
              <p>
                <button type="button" onClick={() => void session.signOut()}>
                  {t("auth.signOut")}
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
  const t = useT();
  return (
    <main className="auth">
      <header>
        <h1>{t("app.brand")}</h1>
        <p>{t("auth.tagline")}</p>
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
  const t = useT();
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
      <h2 id="login-heading">{t("auth.login.title")}</h2>
      <label>
        {t("auth.login.username")}
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
      </label>
      <label>
        {t("auth.login.password")}
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
        {t("auth.login.submit")}
      </button>
      <p className="hint">{t("auth.login.forgot")}</p>
    </form>
  );
}

// CodeForm is the second step of a login: the code of the authenticator app,
// or a recovery code.
function CodeForm({ pending, onDone, onBack }: { pending: string; onDone: (r: LoginResult) => void; onBack: () => void }) {
  const t = useT();
  const [code, setCode] = useState("");
  const { error, busy, submit } = useSubmit(() => loginTwoFactor(pending, code), onDone);
  return (
    <form onSubmit={submit} aria-labelledby="code-heading">
      <h2 id="code-heading">{t("auth.code.title")}</h2>
      <label>
        {t("auth.code.label")}
        <input
          value={code}
          onChange={(e) => setCode(e.target.value)}
          autoComplete="one-time-code"
          inputMode="text"
          autoFocus
          required
        />
      </label>
      <p className="hint">{t("auth.code.hint")}</p>
      {error && <p role="alert">{error}</p>}
      <p className="row-actions">
        <button type="submit" disabled={busy}>
          {t("auth.code.verify")}
        </button>
        <button type="button" onClick={onBack}>
          {t("auth.code.back")}
        </button>
      </p>
    </form>
  );
}

// RecoveryNotice warns that a recovery code was used and how many are left.
function RecoveryNotice({ left, onClose }: { left: number; onClose: () => void }) {
  const t = useT();
  return (
    <p role="status" className="note warn">
      {t(left === 1 ? "auth.recovery.one" : "auth.recovery.many", { count: left })}{" "}
      <button type="button" onClick={onClose}>{t("auth.recovery.dismiss")}</button>
    </p>
  );
}

function SetupForm({ onDone }: { onDone: (result: LoginResult) => void }) {
  const t = useT();
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
      <h2 id="setup-heading">{t("auth.setup.title")}</h2>
      <p>{t("auth.setup.intro")}</p>
      <label>
        {t("auth.setup.token")}
        <input value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        {t("auth.setup.username")}
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
      </label>
      <label>
        {t("auth.setup.password")}
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
        {t("auth.setup.repeat")}
        <input
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
      </label>
      {mismatch && <p role="alert">{t("auth.passwordsMismatch")}</p>}
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {t("auth.setup.submit")}
      </button>
    </form>
  );
}
