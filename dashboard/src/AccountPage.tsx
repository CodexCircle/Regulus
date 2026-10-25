import { useCallback, useState, type FormEvent } from "react";
import { changePassword, fetchSessions, revokeSession, type SessionInfo } from "./api";
import { formatAgo } from "./format";
import { TwoFactor } from "./TwoFactor";
import { errorMessage, useApiList } from "./useApiList";

// AccountPage is where users change their password, set up two-factor authentication and end their sessions.
export function AccountPage() {
  return (
    <section aria-labelledby="account-heading">
      <h2 id="account-heading">Account</h2>
      <PasswordForm />
      <TwoFactor />
      <Sessions />
    </section>
  );
}

function PasswordForm() {
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
      <h3>Change password</h3>
      <label>
        Current password
        <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
      </label>
      <label>
        New password
        <input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />
      </label>
      {status.kind === "error" && <p role="alert">{status.message}</p>}
      {status.kind === "done" && <p role="status">Password changed. Your other sessions were signed out.</p>}
      <button type="submit" disabled={busy}>
        Change password
      </button>
    </form>
  );
}

function Sessions() {
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
      <h3 id="sessions-heading">Your sessions</h3>
      {sessions.kind === "loading" && <p>Loading…</p>}
      {sessions.kind === "error" && <p role="alert">Could not load the sessions: {sessions.message}</p>}
      {error && <p role="alert">{error}</p>}
      {sessions.kind === "ready" && (
        <table>
          <thead>
            <tr>
              <th>Browser</th>
              <th>From</th>
              <th>Last used</th>
              <th>
                <span className="visually-hidden">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {sessions.items.map((s) => (
              <tr key={s.id}>
                <td className="wrap">{s.user_agent || "unknown"}</td>
                <td>{s.remote_addr || "unknown"}</td>
                <td>{s.current ? "now (this session)" : formatAgo(s.last_used_at)}</td>
                <td>
                  {!s.current && (
                    <button type="button" onClick={() => void end(s.id)}>
                      Sign out {s.remote_addr || "session"}
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
