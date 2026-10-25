import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  createReset,
  createUser,
  deleteUser,
  fetchAudit,
  fetchSecurity,
  fetchUsers,
  resetUserTwoFactor,
  saveSecurity,
  updateUser,
  type PasswordReset,
  type Role,
  type SecuritySettings,
  type TwoFactorPolicy,
  type User,
} from "./api";
import { formatAgo } from "./format";
import { useSession } from "./session";
import { errorMessage, useApiList } from "./useApiList";

// UsersPage is where administrators manage accounts and read the audit log.
export function UsersPage() {
  const session = useSession();
  if (session?.user.role !== "admin") {
    return (
      <section aria-labelledby="users-heading">
        <h2 id="users-heading">Users</h2>
        <p role="alert">Only administrators can manage users.</p>
      </section>
    );
  }
  return (
    <section aria-labelledby="users-heading">
      <h2 id="users-heading">Users</h2>
      <UserTable self={session.user.username} />
      <Security />
      <Audit />
    </section>
  );
}

function UserTable({ self }: { self: string }) {
  const load = useCallback((signal: AbortSignal) => fetchUsers(signal), []);
  const [users, reload] = useApiList<User>(load);
  const [error, setError] = useState("");
  const [reset, setReset] = useState<PasswordReset | null>(null);

  // run performs a change, then shows the new list or the hub's refusal.
  const run = async (action: () => Promise<unknown>) => {
    setError("");
    try {
      await action();
    } catch (err) {
      setError(errorMessage(err));
    }
    reload();
  };

  return (
    <>
      {users.kind === "loading" && <p>Loading…</p>}
      {users.kind === "error" && <p role="alert">Could not load users: {users.message}</p>}
      {error && <p role="alert">{error}</p>}
      {users.kind === "ready" && (
        <table>
          <thead>
            <tr>
              <th>User</th>
              <th>Role</th>
              <th>Status</th>
              <th>Two-factor</th>
              <th>Created</th>
              <th>
                <span className="visually-hidden">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {users.items.map((u) => (
              <tr key={u.username}>
                <td>
                  {u.username}
                  {u.username === self && " (you)"}
                </td>
                <td>
                  <select
                    aria-label={`Role of ${u.username}`}
                    value={u.role}
                    disabled={u.username === self}
                    onChange={(e) => void run(() => updateUser(u.username, { role: e.target.value as Role }))}
                  >
                    <option value="admin">admin</option>
                    <option value="viewer">viewer</option>
                  </select>
                </td>
                <td>{u.disabled ? "disabled" : "active"}</td>
                <td>{u.two_factor ? "on" : "off"}</td>
                <td>{formatAgo(u.created_at)}</td>
                <td className="row-actions">
                  <button
                    type="button"
                    disabled={u.username === self}
                    onClick={() => void run(() => updateUser(u.username, { disabled: !u.disabled }))}
                  >
                    {u.disabled ? "Enable" : "Disable"} {u.username}
                  </button>
                  <button
                    type="button"
                    disabled={u.username === self || u.disabled}
                    onClick={() => {
                      if (window.confirm(`Make a password reset link for ${u.username}? It signs ${u.username} out everywhere.`)) {
                        void run(async () => setReset(await createReset(u.username)));
                      }
                    }}
                  >
                    Reset password of {u.username}
                  </button>
                  {u.two_factor && u.username !== self && (
                    <button
                      type="button"
                      onClick={() => {
                        if (window.confirm(`Remove the two-factor authentication of ${u.username}? ${u.username} is signed out everywhere and can sign in with the password alone.`)) {
                          void run(() => resetUserTwoFactor(u.username));
                        }
                      }}
                    >
                      Reset two-factor of {u.username}
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={u.username === self}
                    onClick={() => {
                      if (window.confirm(`Delete the account ${u.username}? This cannot be undone.`)) {
                        void run(() => deleteUser(u.username));
                      }
                    }}
                  >
                    Delete {u.username}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {reset && <ResetLink reset={reset} onClose={() => setReset(null)} />}
      <NewUserForm onCreated={reload} />
    </>
  );
}

// ResetLink shows the link of a password reset once, for the admin to hand to
// the user.
function ResetLink({ reset, onClose }: { reset: PasswordReset; onClose: () => void }) {
  const link = `${window.location.origin}${window.location.pathname}#/reset/${reset.token}`;
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(link).then(
      () => setCopied(true),
      () => setCopied(false),
    );
  };
  return (
    <div className="panel" role="status" aria-label={`Reset link for ${reset.username}`}>
      <h3>Reset link for {reset.username}</h3>
      <p>
        Give this link to {reset.username}. It works once and expires at {new Date(reset.expires_at).toLocaleString()}. It is not shown
        again, and you never see the new password.
      </p>
      <input readOnly value={link} aria-label="Reset link" onFocus={(e) => e.target.select()} />
      <p className="row-actions">
        <button type="button" onClick={copy}>
          {copied ? "Copied" : "Copy link"}
        </button>
        <button type="button" onClick={onClose}>
          Done
        </button>
      </p>
    </div>
  );
}

// Security is where admins choose who must use two-factor authentication.
function Security() {
  const [settings, setSettings] = useState<SecuritySettings | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    fetchSecurity(controller.signal).then(setSettings, (err: unknown) => {
      if (!controller.signal.aborted) setError(errorMessage(err));
    });
    return () => controller.abort();
  }, []);

  const change = async (policy: TwoFactorPolicy) => {
    setError("");
    try {
      setSettings(await saveSecurity(policy));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <section aria-labelledby="security-heading" className="panel">
      <h3 id="security-heading">Security</h3>
      {error && <p role="alert">{error}</p>}
      {settings && (
        <label>
          Who must use two-factor authentication
          <select
            value={settings.two_factor_policy}
            disabled={!settings.two_factor_available}
            onChange={(e) => void change(e.target.value as TwoFactorPolicy)}
          >
            <option value="off">nobody (optional for everyone)</option>
            <option value="admins">administrators</option>
            <option value="all">everyone</option>
          </select>
        </label>
      )}
      <p className="hint">Users it applies to set it up the next time they use Regulus, before anything else.</p>
    </section>
  );
}

function NewUserForm({ onCreated }: { onCreated: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await createUser(username.trim(), password, role);
      setUsername("");
      setPassword("");
      setRole("viewer");
      onCreated();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel" onSubmit={(e) => void submit(e)}>
      <h3>Add a user</h3>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        Password
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={10} required />
      </label>
      <label>
        Role
        <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="viewer">viewer (read only)</option>
          <option value="admin">admin</option>
        </select>
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        Add user
      </button>
    </form>
  );
}

function Audit() {
  const load = useCallback((signal: AbortSignal) => fetchAudit(100, signal), []);
  const [entries, reload] = useApiList(load);
  return (
    <section aria-labelledby="audit-heading" className="panel">
      <h3 id="audit-heading">Recent activity</h3>
      <button type="button" onClick={reload}>
        Refresh
      </button>
      {entries.kind === "loading" && <p>Loading…</p>}
      {entries.kind === "error" && <p role="alert">Could not load the activity: {entries.message}</p>}
      {entries.kind === "ready" && entries.items.length === 0 && <p>Nothing yet.</p>}
      {entries.kind === "ready" && entries.items.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>Who</th>
              <th>Event</th>
              <th>Target</th>
              <th>From</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {entries.items.map((a) => (
              <tr key={a.id}>
                <td>{formatAgo(a.at)}</td>
                <td>{a.actor || "–"}</td>
                <td>{a.event.replace(/_/g, " ")}</td>
                <td>{a.target || "–"}</td>
                <td>{a.remote || "–"}</td>
                <td>{a.detail || "–"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

