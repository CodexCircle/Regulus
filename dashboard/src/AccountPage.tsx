import { useCallback, useState, type FormEvent } from "react";
import { changePassword, fetchSessions, revokeSession, type SessionInfo } from "./api";
import { formatAgo } from "./format";
import { TwoFactor } from "./TwoFactor";
import { useT } from "./i18n";
import { errorMessage, useApiList } from "./useApiList";

// AccountPage is where users change their password, set up two-factor authentication and end their sessions.
export function AccountPage() {
  const t = useT();
  return (
    <section aria-labelledby="account-heading">
      <h2 id="account-heading">{t("account.title")}</h2>
      <PasswordForm />
      <TwoFactor />
      <Sessions />
    </section>
  );
}

function PasswordForm() {
  const t = useT();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [status, setStatus] = useState<{ kind: "idle" } | { kind: "done" } | { kind: "error"; message: string }>({ kind: "idle" });
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await changePassword(current, next);
      setCurrent("");
      setNext("");
      setStatus({ kind: "done" });
    } catch (err) {
      setStatus({ kind: "error", message: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel" onSubmit={(e) => void submit(e)}>
      <h3>{t("account.password.title")}</h3>
      <label>
        {t("account.password.current")}
        <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
      </label>
      <label>
        {t("account.password.new")}
        <input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />
      </label>
      {status.kind === "error" && <p role="alert">{status.message}</p>}
      {status.kind === "done" && <p role="status">{t("account.password.changed")}</p>}
      <button type="submit" disabled={busy}>
        {t("account.password.submit")}
      </button>
    </form>
  );
}

function Sessions() {
  const t = useT();
  const load = useCallback((signal: AbortSignal) => fetchSessions(signal), []);
  const [sessions, reload] = useApiList<SessionInfo>(load);
  const [error, setError] = useState("");

  const end = async (id: string) => {
    setError("");
    try {
      await revokeSession(id);
    } catch (err) {
      setError(errorMessage(err));
    }
    reload();
  };

  return (
    <section aria-labelledby="sessions-heading" className="panel">
      <h3 id="sessions-heading">{t("account.sessions.title")}</h3>
      {sessions.kind === "loading" && <p>{t("account.loading")}</p>}
      {sessions.kind === "error" && <p role="alert">{t("account.sessions.loadError", { message: sessions.message })}</p>}
      {error && <p role="alert">{error}</p>}
      {sessions.kind === "ready" && (
        <table>
          <thead>
            <tr>
              <th>{t("account.sessions.browser")}</th>
              <th>{t("account.sessions.from")}</th>
              <th>{t("account.sessions.lastUsed")}</th>
              <th>
                <span className="visually-hidden">{t("account.sessions.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {sessions.items.map((s) => (
              <tr key={s.id}>
                <td className="wrap">{s.user_agent || t("account.sessions.unknown")}</td>
                <td>{s.remote_addr || t("account.sessions.unknown")}</td>
                <td>{s.current ? t("account.sessions.thisOne") : formatAgo(s.last_used_at, undefined, t)}</td>
                <td>
                  {!s.current && (
                    <button type="button" onClick={() => void end(s.id)}>
                      {s.remote_addr ? t("account.sessions.signOutFrom", { from: s.remote_addr }) : t("account.sessions.signOutSession")}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
