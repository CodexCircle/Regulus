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
import { useT } from "./i18n";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

type Load = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; status: TwoFactorStatus };

// A recovery code shows as five letters, a dash and five more.
const FEW_CODES_LEFT = 3;

// TwoFactor lets users turn two-factor authentication (an authenticator app)
// on and off and make new recovery codes. onChange is called after the state
// changed, so a caller that waits for it can go on.
export function TwoFactor({ onChange }: { onChange?: () => void }) {
  const t = useT();
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
      <h3 id="two-factor-heading">{t("account.twoFactor.title")}</h3>
      {load.kind === "loading" && <p>{t("account.loading")}</p>}
      {load.kind === "error" && <p role="alert">{t("account.twoFactor.loadError", { message: load.message })}</p>}
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
  const t = useT();
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
            {t("account.twoFactor.mandatoryNotice")}
          </p>
        ) : (
          <p>{t("account.twoFactor.intro")}</p>
        )}
        <label>
          {t("account.twoFactor.yourPassword")}
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={busy}>
          {t("account.twoFactor.setUp")}
        </button>
      </form>
    );
  }

  return (
    <form onSubmit={(e) => void confirm(e)}>
      <p>{t("account.twoFactor.scan")}</p>
      <img className="qr" src={setup.qr} alt={t("account.twoFactor.qrAlt")} width={180} height={180} />
      <p>
        {t("account.twoFactor.key")} <code aria-label={t("account.twoFactor.secretKey")}>{setup.secret}</code>
      </p>
      <label>
        {t("account.twoFactor.enterCode")}
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
        {t("account.twoFactor.turnOn")}
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
  const t = useT();
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
      <p role="status">{t("account.twoFactor.isOn")}</p>
      <p>
        {t("account.twoFactor.codesLeft")} <strong>{status.recovery_codes_left}</strong>
      </p>
      {status.recovery_codes_left <= FEW_CODES_LEFT && (
        <p role="alert" className="note warn">
          {t("account.twoFactor.runningOut")}
        </p>
      )}
      {action === "" ? (
        <p className="row-actions">
          <button type="button" onClick={() => setAction("renew")}>
            {t("account.twoFactor.makeNew")}
          </button>
          {status.mandatory ? (
            <span className="muted">{t("account.twoFactor.cannotTurnOff")}</span>
          ) : (
            <button type="button" onClick={() => setAction("disable")}>
              {t("account.twoFactor.turnOff")}
            </button>
          )}
        </p>
      ) : (
        <form onSubmit={(e) => void submit(e)}>
          <p>
            {action === "renew"
              ? t("account.twoFactor.renewWarning")
              : t("account.twoFactor.disableWarning")}{" "}
            {t("account.twoFactor.confirmHint")}
          </p>
          <label>
            {t("account.twoFactor.yourPassword")}
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
          </label>
          <label>
            {t("account.twoFactor.codeOrRecovery")}
            <input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" required />
          </label>
          {error && <p role="alert">{error}</p>}
          <p className="row-actions">
            <button type="submit" disabled={busy}>
              {action === "renew" ? t("account.twoFactor.makeNew") : t("account.twoFactor.turnOffShort")}
            </button>
            <button type="button" onClick={close}>
              {t("account.twoFactor.cancel")}
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
  const t = useT();
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
    const url = URL.createObjectURL(new Blob([`${t("account.recovery.fileHeader")}\n\n${text}\n`], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "regulus-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div role="status" aria-label={t("account.recovery.label")}>
      <p>
        <strong>{t("account.recovery.saveNow")}</strong> {t("account.recovery.explain")}
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
          {copied ? t("account.recovery.copied") : t("account.recovery.copy")}
        </button>
        <button type="button" onClick={download}>
          {t("account.recovery.download")}
        </button>
      </p>
      <label className="check">
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />{" "}
        {t("account.recovery.saved")}
      </label>
      <button type="button" disabled={!saved} onClick={onDone}>
        {t("account.recovery.done")}
      </button>
    </div>
  );
}
