import { useState, type FormEvent } from "react";
import { resetPassword } from "./api";
import { useT } from "./i18n";
import { resetTokenFromHash } from "./route";
import { errorMessage } from "./useApiList";

// ResetPage is where the owner of an account opens a reset link and chooses a
// new password.
export function ResetPage() {
  const t = useT();
  const token = resetTokenFromHash(window.location.hash);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (password !== confirm) {
      setError(t("auth.passwordsMismatch"));
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
        <h2 id="reset-heading">{t("auth.reset.doneTitle")}</h2>
        <p role="status">{t("auth.reset.done")}</p>
        <p>
          <a href="#/">{t("auth.reset.signIn")}</a>
        </p>
      </section>
    );
  }

  return (
    <form onSubmit={(e) => void submit(e)} aria-labelledby="reset-heading">
      <h2 id="reset-heading">{t("auth.reset.title")}</h2>
      <p>{t("auth.reset.intro")}</p>
      <label>
        {t("auth.reset.password")}
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={10} required />
      </label>
      <label>
        {t("auth.reset.repeat")}
        <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {t("auth.reset.submit")}
      </button>
    </form>
  );
}
