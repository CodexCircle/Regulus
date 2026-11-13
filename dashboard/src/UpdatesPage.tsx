import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  applyUpdate,
  checkForUpdates,
  downloadUpdate,
  fetchRollout,
  fetchUpdates,
  startRollout,
  stopRollout,
  saveUpdateSettings,
  type RolloutAgent,
  type RolloutStatus,
  type UpdateChannel,
  type UpdateMode,
  type UpdateStatus,
} from "./api";
import { formatAgo, formatBytes } from "./format";
import { useT, type MessageKey } from "./i18n";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

const POLL_MS = 2000;

// UpdatesPage shows the version of the hub, whether a newer release exists, and
// lets those with core.update check, download and install it and choose how the
// hub behaves. Installing is done by a root helper of the hub's server, which
// checks the release again; the hub restarts in the middle of it.
export function UpdatesPage() {
  const t = useT();
  const session = useSession();
  const expired = session?.expired;
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // fail ends the login on a 401 and shows anything else.
  const fail = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && err.status === 401) expired?.();
      else setError(errorMessage(err));
    },
    [expired],
  );

  useEffect(() => {
    const controller = new AbortController();
    fetchUpdates(controller.signal).then(setStatus, (err: unknown) => {
      if (!controller.signal.aborted) fail(err);
    });
    return () => controller.abort();
  }, [fail]);

  // While the hub works in the background, ask again until it is done.
  const working = status?.downloading || status?.checking || status?.applying;
  useEffect(() => {
    if (!working) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      fetchUpdates(controller.signal).then(setStatus, () => {
        // keep what is shown; the next round tries again
      });
    }, POLL_MS);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [working]);

  const run = async (action: () => Promise<UpdateStatus>) => {
    setError("");
    setBusy(true);
    try {
      setStatus(await action());
    } catch (err) {
      fail(err);
      // The hub may have recorded why (check_error); show it too.
      fetchUpdates().then(setStatus, () => undefined);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-labelledby="updates-heading">
      <h2 id="updates-heading">{t("updates.title")}</h2>
      {error && <p role="alert">{error}</p>}
      {status === null ? (
        !error && <p role="status">{t("updates.loading")}</p>
      ) : (
        <>
          <Version
            status={status}
            busy={busy}
            onCheck={() => void run(checkForUpdates)}
            onDownload={() => void run(downloadUpdate)}
            onApply={() => run(applyUpdate)}
          />
          <Servers />
          <Settings
            key={`${status.mode}/${status.channel}/${status.window_start ?? ""}/${status.window_end ?? ""}`}
            status={status}
            onSave={(mode, channel, window) =>
              run(() => saveUpdateSettings(mode, channel, window))
            }
          />
        </>
      )}
    </section>
  );
}

function Version({
  status,
  busy,
  onCheck,
  onDownload,
  onApply,
}: {
  status: UpdateStatus;
  busy: boolean;
  onCheck: () => void;
  onDownload: () => void;
  onApply: () => Promise<void>;
}) {
  const t = useT();
  const { latest, staged, last_apply: last } = status;
  const channelName = (c: UpdateChannel) => t(`updates.channel.${c}`);
  const [confirming, setConfirming] = useState(false);
  const downloaded =
    staged !== null && latest !== null && staged.version === latest.version;
  return (
    <section className="panel" aria-labelledby="version-heading">
      <h3 id="version-heading">{t("updates.version.title")}</h3>
      <p>
        {t("updates.version.runs")}{" "}
        <strong>{status.current_version || t("updates.unknown")}</strong>{" "}
        {t("updates.version.platform", { platform: status.platform })}
      </p>
      {!status.release_build && (
        <p className="muted">{t("updates.version.devBuild")}</p>
      )}
      {!status.checks_enabled && (
        <p role="status">
          {t("updates.version.checksOffBefore")}
          <code>updates.check</code>
          {t("updates.version.checksOffAfter")}
        </p>
      )}

      {status.checks_enabled && (
        <>
          {status.update_available && latest ? (
            <div role="status" aria-label={t("updates.version.availableLabel")}>
              <p>
                <strong>
                  {t("updates.version.availableVersion", {
                    version: latest.version,
                  })}
                </strong>
                {latest.channel !== "stable" && ` (${latest.channel})`}{" "}
                {t("updates.version.isAvailable")}
                {latest.url.startsWith("https://") && (
                  <>
                    {" "}
                    <a href={latest.url} target="_blank" rel="noreferrer">
                      {t("updates.version.releasePage")}
                    </a>
                  </>
                )}
              </p>
              {latest.notes && <pre className="notes">{latest.notes}</pre>}
            </div>
          ) : (
            status.release_build &&
            latest && (
              <p role="status">
                {t("updates.version.upToDate", {
                  channel: channelName(status.channel),
                  version: latest.version,
                })}
              </p>
            )
          )}
          {status.release_build &&
            !latest &&
            status.checked_at &&
            !status.check_error && (
              <p role="status">
                {t("updates.version.noRelease", {
                  channel: channelName(status.channel),
                })}
              </p>
            )}
          <p className="muted">
            {status.checked_at
              ? t("updates.version.checked", {
                  ago: formatAgo(status.checked_at),
                })
              : t("updates.version.notChecked")}
          </p>
          {status.check_error && (
            <p role="alert">
              {t("updates.version.checkFailed", { error: status.check_error })}
            </p>
          )}
        </>
      )}

      {staged && (
        <div role="status" aria-label={t("updates.staged.label")}>
          <p>
            {t("updates.staged.version")} <strong>{staged.version}</strong>{" "}
            {t("updates.staged.downloaded", {
              size: formatBytes(staged.size),
              archive: staged.archive,
            })}{" "}
            {staged.verified === "signature"
              ? t("updates.staged.signature")
              : t("updates.staged.checksum")}{" "}
            {t("updates.staged.itIs")}{" "}
            <strong>{t("updates.staged.notInstalled")}</strong>
            {status.apply_enabled
              ? t("updates.staged.applyOn")
              : t("updates.staged.applyOff")}
          </p>
          {staged.verified !== "signature" && (
            <p className="muted">{t("updates.staged.checksumNote")}</p>
          )}
        </div>
      )}
      {status.downloading && <p role="status">{t("updates.downloading")}</p>}
      {status.applying && (
        <p role="status">
          {t("updates.applying.head", {
            what: staged?.version ?? t("updates.applying.theUpdate"),
          })}{" "}
          {t("updates.applying.rest")}
        </p>
      )}
      {status.apply_stalled && (
        <p role="alert">
          {t("updates.stalled")}{" "}
          <code>systemctl enable --now regulus-update.path</code>.
        </p>
      )}
      {last && !status.applying && last.outcome === "applied" && (
        <p role="status">
          {last.from
            ? t("updates.last.appliedFrom", {
                version: last.version,
                from: last.from,
                ago: formatAgo(last.at),
              })
            : t("updates.last.applied", {
                version: last.version,
                ago: formatAgo(last.at),
              })}
        </p>
      )}
      {last && !status.applying && last.outcome !== "applied" && (
        <p role="alert">
          {last.outcome === "rolled_back"
            ? t("updates.last.rolledBack", { version: last.version })
            : t("updates.last.notInstalled", { version: last.version })}
          {last.message ? ` ${last.message}` : ""}
        </p>
      )}
      {status.download_error && (
        <p role="alert">
          {t("updates.downloadFailed", { error: status.download_error })}
        </p>
      )}

      <p className="row-actions">
        <button
          type="button"
          onClick={onCheck}
          disabled={busy || status.checking || !status.checks_enabled}
        >
          {status.checking
            ? t("updates.action.checking")
            : t("updates.action.checkNow")}
        </button>
        {status.update_available && latest && !downloaded && (
          <button
            type="button"
            onClick={onDownload}
            disabled={busy || status.downloading}
          >
            {t("updates.action.download", { version: latest.version })}
          </button>
        )}
        {status.apply_available && staged && !confirming && (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            disabled={busy}
          >
            {t("updates.action.install", { version: staged.version })}
          </button>
        )}
      </p>
      {confirming && staged && status.apply_available && (
        <div role="group" aria-label={t("updates.confirm.label")}>
          <p>{t("updates.confirm.text", { version: staged.version })}</p>
          <p className="row-actions">
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                setConfirming(false);
                void onApply();
              }}
            >
              {t("updates.action.installNow")}
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              {t("updates.action.cancel")}
            </button>
          </p>
        </div>
      )}
    </section>
  );
}

function Settings({
  status,
  onSave,
}: {
  status: UpdateStatus;
  onSave: (
    mode: UpdateMode,
    channel: UpdateChannel,
    window: { start: string; end: string },
  ) => Promise<void>;
}) {
  const t = useT();
  const [mode, setMode] = useState<UpdateMode>(status.mode);
  const [channel, setChannel] = useState<UpdateChannel>(status.channel);
  const [start, setStart] = useState(status.window_start || "03:00");
  const [end, setEnd] = useState(status.window_end || "05:00");
  const windowChanged =
    mode === "automatic" &&
    (start !== status.window_start || end !== status.window_end);
  const changed =
    mode !== status.mode || channel !== status.channel || windowChanged;
  const valid =
    mode !== "automatic" || (start !== "" && end !== "" && start !== end);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void onSave(mode, channel, { start, end });
  };

  return (
    <section className="panel" aria-labelledby="update-settings-heading">
      <h3 id="update-settings-heading">{t("updates.settings.title")}</h3>
      <form onSubmit={submit}>
        <fieldset>
          <legend>{t("updates.settings.when")}</legend>
          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="manual"
              checked={mode === "manual"}
              onChange={() => setMode("manual")}
            />
            {t("updates.mode.manual")}
          </label>
          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="download"
              checked={mode === "download"}
              onChange={() => setMode("download")}
            />
            {t("updates.mode.download")}
          </label>
          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="automatic"
              checked={mode === "automatic"}
              disabled={!status.apply_enabled}
              onChange={() => setMode("automatic")}
            />
            {t("updates.mode.automatic")}
          </label>
          {!status.apply_enabled && (
            <p className="muted">{t("updates.settings.applyOff")}</p>
          )}
          {mode === "automatic" && (
            <p className="row-actions">
              <label>
                {t("updates.settings.windowStarts")}
                <input
                  type="time"
                  value={start}
                  onChange={(e) => setStart(e.target.value)}
                />
              </label>
              <label>
                {t("updates.settings.windowEnds")}
                <input
                  type="time"
                  value={end}
                  onChange={(e) => setEnd(e.target.value)}
                />
              </label>
            </p>
          )}
          {mode === "automatic" && (
            <p className="muted">{t("updates.settings.windowNote")}</p>
          )}
        </fieldset>
        <label>
          {t("updates.settings.channel")}
          <select
            value={channel}
            onChange={(e) => setChannel(e.target.value as UpdateChannel)}
          >
            <option value="stable">
              {t("updates.settings.channelStable")}
            </option>
            <option value="beta">{t("updates.settings.channelBeta")}</option>
            <option value="alpha">{t("updates.settings.channelAlpha")}</option>
          </select>
        </label>
        <button type="submit" disabled={!changed || !valid}>
          {t("updates.action.save")}
        </button>
      </form>
    </section>
  );
}

const AGENT_STATUS: Record<RolloutAgent["status"], MessageKey> = {
  pending: "updates.agent.pending",
  offered: "updates.agent.offered",
  installing: "updates.agent.installing",
  updated: "updates.agent.updated",
  failed: "updates.agent.failed",
  skipped: "updates.agent.skipped",
};

// Servers shows the managed servers that run an older version than the hub and
// updates them to it, a few at a time, stopping at the first failure.
function Servers() {
  const t = useT();
  const session = useSession();
  const expired = session?.expired;
  const [rollout, setRollout] = useState<RolloutStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [wave, setWave] = useState(1);

  const fail = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && err.status === 401) expired?.();
      else setError(errorMessage(err));
    },
    [expired],
  );

  useEffect(() => {
    const controller = new AbortController();
    fetchRollout(controller.signal).then(setRollout, (err: unknown) => {
      if (!controller.signal.aborted) fail(err);
    });
    return () => controller.abort();
  }, [fail]);

  const running = rollout?.state === "running";
  useEffect(() => {
    if (!running) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      fetchRollout(controller.signal).then(setRollout, () => undefined);
    }, POLL_MS);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [running]);

  const run = async (action: () => Promise<RolloutStatus>) => {
    setError("");
    setBusy(true);
    try {
      setRollout(await action());
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  if (rollout === null) return error ? <p role="alert">{error}</p> : null;

  const behind = rollout.candidates;
  const finished = rollout.state !== "idle" && rollout.state !== "running";
  return (
    <section className="panel" aria-labelledby="servers-heading">
      <h3 id="servers-heading">{t("updates.rollout.title")}</h3>
      {error && <p role="alert">{error}</p>}
      <p>
        {t("updates.rollout.intro")}
        <strong>{rollout.target || t("updates.unknown")}</strong>
        {t("updates.rollout.introAfter")}
      </p>

      {rollout.state === "running" && (
        <>
          <p role="status">
            {t("updates.rollout.running", { size: rollout.wave_size ?? 1 })}
          </p>
          <AgentRows
            agents={rollout.agents}
            label={t("updates.rollout.progress")}
          />
          <p className="row-actions">
            <button
              type="button"
              onClick={() => void run(stopRollout)}
              disabled={busy}
            >
              {t("updates.action.stop")}
            </button>
          </p>
        </>
      )}

      {rollout.state !== "running" && (
        <>
          {behind.length === 0 ? (
            <p role="status">
              {t("updates.rollout.allCurrent", { target: rollout.target })}
            </p>
          ) : (
            <>
              <p role="status">
                {behind.length === 1
                  ? t("updates.rollout.behindOne")
                  : t("updates.rollout.behindMany", { count: behind.length })}
              </p>
              <ul aria-label={t("updates.rollout.behindLabel")}>
                {behind.map((a) => (
                  <li key={a.id}>
                    {t("updates.rollout.runs", {
                      name: a.hostname || a.id,
                      version: a.from,
                    })}
                  </li>
                ))}
              </ul>
              <p className="row-actions">
                <label>
                  {t("updates.rollout.atATime")}
                  <select
                    value={wave}
                    onChange={(e) => setWave(Number(e.target.value))}
                  >
                    <option value={1}>1</option>
                    <option value={3}>3</option>
                    <option value={10}>10</option>
                    <option value={100}>{t("updates.rollout.all")}</option>
                  </select>
                </label>
                <button
                  type="button"
                  onClick={() => void run(() => startRollout(wave))}
                  disabled={busy}
                >
                  {behind.length === 1
                    ? t("updates.rollout.updateOne", { target: rollout.target })
                    : t("updates.rollout.updateMany", {
                        count: behind.length,
                        target: rollout.target,
                      })}
                </button>
              </p>
            </>
          )}
          {finished && rollout.agents.length > 0 && (
            <>
              <p className="muted">
                {t(
                  rollout.state === "done"
                    ? rollout.finished_at
                      ? "updates.rollout.lastDoneAgo"
                      : "updates.rollout.lastDone"
                    : rollout.state === "stopped"
                      ? rollout.finished_at
                        ? "updates.rollout.lastStoppedAgo"
                        : "updates.rollout.lastStopped"
                      : rollout.finished_at
                        ? "updates.rollout.lastFailedAgo"
                        : "updates.rollout.lastFailed",
                  {
                    ago: rollout.finished_at
                      ? formatAgo(rollout.finished_at)
                      : "",
                  },
                )}
              </p>
              <AgentRows
                agents={rollout.agents}
                label={t("updates.rollout.lastLabel")}
              />
            </>
          )}
        </>
      )}
    </section>
  );
}

function AgentRows({
  agents,
  label,
}: {
  agents: RolloutAgent[];
  label: string;
}) {
  const t = useT();
  return (
    <ul aria-label={label}>
      {agents.map((a) => (
        <li key={a.id}>
          {t(
            a.message && a.status === "failed"
              ? "updates.agent.rowMessage"
              : "updates.agent.row",
            {
              name: a.hostname || a.id,
              from: a.from,
              status: t(AGENT_STATUS[a.status]),
              message: a.message ?? "",
            },
          )}
        </li>
      ))}
    </ul>
  );
}
