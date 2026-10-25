import { useState, type FormEvent } from "react";
import { resetPassword } from "./api";
import { resetTokenFromHash } from "./route";
import { errorMessage } from "./useApiList";

// ResetPage is where the owner of an account opens a reset link and chooses a
// new password.
export function ResetPage() {
  const token = resetTokenFromHash(window.location.hash);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (password !== confirm) {
      setError("The passwords do not match.");
      return;
    }
    setError("");
    setBusy(true);
    try {
      await resetPassword(token, password);
      setDone(true);
      // Take the token out of the address bar and the history.
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <section aria-labelledby="reset-heading">
        <h2 id="reset-heading">Password changed</h2>
        <p role="status">Your password was changed and all your sessions were signed out.</p>
        <p>
          <a href="#/">Sign in</a>
        </p>
      </section>
    );
  }

  return (
    <form onSubmit={(e) => void submit(e)} aria-labelledby="reset-heading">
      <h2 id="reset-heading">Choose a new password</h2>
      <p>The link works once and expires an hour after the administrator made it.</p>
      <label>
        New password (at least 10 characters)
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={10} required />
      </label>
      <label>
        Repeat the password
        <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        Change password
      </button>
    </form>
  );
}
