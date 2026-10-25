import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  disableTwoFactor,
  enableTwoFactor,
  fetchTwoFactor,
  renewRecoveryCodes,
  startTwoFactor,
  type TwoFactorSetup,
  type TwoFactorStatus,
} from "./api";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

type Load = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; status: TwoFactorStatus };

// A recovery code shows as five letters, a dash and five more.
const FEW_CODES_LEFT = 3;

// TwoFactor lets users turn two-factor authentication (an authenticator app)
// on and off and make new recovery codes. onChange is called after the state
// changed, so a caller that waits for it can go on.
export function TwoFactor({ onChange }: { onChange?: () => void }) {
  const session = useSession();
  const expired = session?.expired;
  const [load, setLoad] = useState<Load>({ kind: "loading" });
  // Recovery codes just made: shown once, until the user says they are saved.
  const [codes, setCodes] = useState<string[] | null>(null);

  const refresh = useCallback(() => {
    fetchTwoFactor().then(
      (status) => setLoad({ kind: "ready", status }),
      (err: unknown) => {
        if (err instanceof ApiError && err.status === 401) return expired?.();
        setLoad({ kind: "error", message: errorMessage(err) });
      },
    );
  }, [expired]);

  useEffect(refresh, [refresh]);

  const changed = () => {
    refresh();
    onChange?.();
  };

  return (
    <section className="panel" aria-labelledby="two-factor-heading">
      <h3 id="two-factor-heading">Two-factor authentication</h3>
      {load.kind === "loading" && <p>Loading…</p>}
      {load.kind === "error" && <p role="alert">Could not load the state: {load.message}</p>}
      {codes && (
        <RecoveryCodes
          codes={codes}
          onDone={() => {
            setCodes(null);
            onChange?.();
          }}
        />
      )}
      {load.kind === "ready" && !codes && load.status.enabled && (
        <Enabled
          status={load.status}
          onCodes={(c) => {
            setCodes(c);
            refresh();
          }}
          onDisabled={changed}
        />
      )}
      {load.kind === "ready" && !codes && !load.status.enabled && (
        <Disabled
          mandatory={load.status.mandatory}
          onEnabled={(c) => {
            setCodes(c);
            refresh();
          }}
        />
      )}
    </section>
  );
}

function Disabled({ mandatory, onEnabled }: { mandatory: boolean; onEnabled: (codes: string[]) => void }) {
  const [password, setPassword] = useState("");
  const [setup, setSetup] = useState<TwoFactorSetup | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const start = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      setSetup(await startTwoFactor(password));
      setPassword("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const confirm = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      onEnabled(await enableTwoFactor(code));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (!setup) {
    return (
      <form onSubmit={(e) => void start(e)}>
        {mandatory ? (
          <p role="status" className="note warn">
            This hub requires two-factor authentication. Set it up to continue.
          </p>
        ) : (
          <p>Add a second step to your sign-in: a code from an authenticator app on your phone, besides your password.</p>
        )}
        <label>
          Your password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={busy}>
          Set up two-factor authentication
        </button>
      </form>
    );
  }

  return (
    <form onSubmit={(e) => void confirm(e)}>
      <p>
        1. Scan this code with an authenticator app (Google Authenticator, Aegis, 1Password, Bitwarden…), or type the key into it.
      </p>
      <img className="qr" src={setup.qr} alt="QR code to add this account to an authenticator app" width={180} height={180} />
      <p>
        Key: <code aria-label="Secret key">{setup.secret}</code>
      </p>
      <label>
        2. Enter the 6-digit code the app shows
        <input
          value={code}
          onChange={(e) => setCode(e.target.value)}
          inputMode="numeric"
          autoComplete="one-time-code"
          pattern="[0-9 ]*"
          required
        />
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        Turn on two-factor authentication
      </button>
    </form>
  );
}

function Enabled({
  status,
  onCodes,
  onDisabled,
}: {
  status: TwoFactorStatus;
  onCodes: (codes: string[]) => void;
  onDisabled: () => void;
}) {
  const [action, setAction] = useState<"" | "renew" | "disable">("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const close = () => {
    setAction("");
    setPassword("");
    setCode("");
    setError("");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (action === "renew") {
        onCodes(await renewRecoveryCodes(password, code));
      } else {
        await disableTwoFactor(password, code);
        onDisabled();
      }
      close();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <p role="status">Two-factor authentication is on. You need a code from your app each time you sign in.</p>
      <p>
        Recovery codes left: <strong>{status.recovery_codes_left}</strong>
      </p>
      {status.recovery_codes_left <= FEW_CODES_LEFT && (
        <p role="alert" className="note warn">
          You are running out of recovery codes. Make new ones and keep them somewhere safe.
        </p>
      )}
      {action === "" ? (
        <p className="row-actions">
          <button type="button" onClick={() => setAction("renew")}>
            Make new recovery codes
          </button>
          {status.mandatory ? (
            <span className="muted">The hub requires two-factor authentication, so it cannot be turned off.</span>
          ) : (
            <button type="button" onClick={() => setAction("disable")}>
              Turn off two-factor authentication
            </button>
          )}
        </p>
      ) : (
        <form onSubmit={(e) => void submit(e)}>
          <p>
            {action === "renew"
              ? "The old recovery codes stop working."
              : "Your account will be protected by the password alone."}{" "}
            Confirm with your password and a code.
          </p>
          <label>
            Your password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
          </label>
          <label>
            Code from the app, or a recovery code
            <input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" required />
          </label>
          {error && <p role="alert">{error}</p>}
          <p className="row-actions">
            <button type="submit" disabled={busy}>
              {action === "renew" ? "Make new recovery codes" : "Turn off"}
            </button>
            <button type="button" onClick={close}>
              Cancel
            </button>
          </p>
        </form>
      )}
    </>
  );
}

// RecoveryCodes shows the codes once. They are the way back in when the phone
// is lost, so the user has to say they saved them before going on.
function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  const text = codes.join("\n");

  const copy = () => {
    navigator.clipboard.writeText(text).then(
      () => setCopied(true),
      () => setCopied(false),
    );
  };
  const download = () => {
    const url = URL.createObjectURL(new Blob([`Regulus recovery codes\n\n${text}\n`], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "regulus-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div role="status" aria-label="Recovery codes">
      <p>
        <strong>Save these recovery codes now.</strong> Each works once, instead of a code from the app, if you lose your phone. They are
        not shown again.
      </p>
      <ul className="recovery-codes">
        {codes.map((c) => (
          <li key={c}>
            <code>{c}</code>
          </li>
        ))}
      </ul>
      <p className="row-actions">
        <button type="button" onClick={copy}>
          {copied ? "Copied" : "Copy codes"}
        </button>
        <button type="button" onClick={download}>
          Download codes
        </button>
      </p>
      <label className="check">
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} /> I saved these codes somewhere safe
      </label>
      <button type="button" disabled={!saved} onClick={onDone}>
        Done
      </button>
    </div>
  );
}
