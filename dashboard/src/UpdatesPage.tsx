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
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

const POLL_MS = 2000;

// UpdatesPage shows the version of the hub, whether a newer release exists, and
// lets those with core.update check, download and install it and choose how the
// hub behaves. Installing is done by a root helper of the hub's server, which
// checks the release again; the hub restarts in the middle of it.
export function UpdatesPage() {
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
      <h2 id="updates-heading">Updates</h2>
      {error && <p role="alert">{error}</p>}
      {status === null ? (
        !error && <p role="status">Loading…</p>
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
  const { latest, staged, last_apply: last } = status;
  const [confirming, setConfirming] = useState(false);
  const downloaded =
    staged !== null && latest !== null && staged.version === latest.version;
  return (
    <section className="panel" aria-labelledby="version-heading">
      <h3 id="version-heading">Version</h3>
      <p>
        This hub runs <strong>{status.current_version || "unknown"}</strong> (
        {status.platform}).
      </p>
      {!status.release_build && (
        <p className="muted">
          This is a development build. It does not compare itself with releases,
          so it is never offered an update.
        </p>
      )}
      {!status.checks_enabled && (
        <p role="status">
          Update checks are turned off in the configuration (
          <code>updates.check</code>), so the hub does not contact any server.
        </p>
      )}

      {status.checks_enabled && (
        <>
          {status.update_available && latest ? (
            <div role="status" aria-label="Update available">
              <p>
                <strong>Version {latest.version}</strong>
                {latest.prerelease && " (beta)"} is available.
                {latest.url.startsWith("https://") && (
                  <>
                    {" "}
                    <a href={latest.url} target="_blank" rel="noreferrer">
                      Release page
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
                Regulus is up to date: the newest {status.channel} release is{" "}
                {latest.version}.
              </p>
            )
          )}
          {status.release_build &&
            !latest &&
            status.checked_at &&
            !status.check_error && (
              <p role="status">
                The release server lists no {status.channel} release.
              </p>
            )}
          <p className="muted">
            {status.checked_at
              ? `Checked ${formatAgo(status.checked_at)}.`
              : "Not checked yet."}
          </p>
          {status.check_error && (
            <p role="alert">The last check failed: {status.check_error}</p>
          )}
        </>
      )}

      {staged && (
        <div role="status" aria-label="Downloaded update">
          <p>
            Version <strong>{staged.version}</strong> is downloaded (
            {formatBytes(staged.size)}, {staged.archive}).{" "}
            {staged.verified === "signature"
              ? "Its signature was checked: it was made by this project's release workflow for this version, and the file matches it."
              : "Its SHA-256 matches the release's checksum list."}{" "}
            It is <strong>not installed</strong>
            {status.apply_enabled
              ? "."
              : ": this hub was not set up to install updates itself (updates.apply, and the regulus-update.path unit of the installer)."}
          </p>
          {staged.verified !== "signature" && (
            <p className="muted">
              The checksum list comes from the same place as the file, so it
              catches a damaged download but does not prove who published it.
            </p>
          )}
        </div>
      )}
      {status.downloading && <p role="status">Downloading…</p>}
      {status.applying && (
        <p role="status">
          Installing {staged?.version ?? "the update"}… The hub restarts in a
          moment and this page may stop answering for a short while; it checks
          again by itself. If the new version does not start properly, the
          previous one is put back.
        </p>
      )}
      {status.apply_stalled && (
        <p role="alert">
          Nothing has picked up the request for a few minutes. The unit that
          installs updates is probably not running:{" "}
          <code>systemctl enable --now regulus-update.path</code>.
        </p>
      )}
      {last && !status.applying && last.outcome === "applied" && (
        <p role="status">
          Updated to {last.version}
          {last.from ? ` from ${last.from}` : ""} {formatAgo(last.at)}.
        </p>
      )}
      {last && !status.applying && last.outcome !== "applied" && (
        <p role="alert">
          {last.outcome === "rolled_back"
            ? `Version ${last.version} did not start properly, so the previous version was put back.`
            : `Version ${last.version} was not installed.`}
          {last.message ? ` ${last.message}` : ""}
        </p>
      )}
      {status.download_error && (
        <p role="alert">The download failed: {status.download_error}</p>
      )}

      <p className="row-actions">
        <button
          type="button"
          onClick={onCheck}
          disabled={busy || status.checking || !status.checks_enabled}
        >
          {status.checking ? "Checking…" : "Check now"}
        </button>
        {status.update_available && latest && !downloaded && (
          <button
            type="button"
            onClick={onDownload}
            disabled={busy || status.downloading}
          >
            Download {latest.version}
          </button>
        )}
        {status.apply_available && staged && !confirming && (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            disabled={busy}
          >
            Install {staged.version}
          </button>
        )}
      </p>
      {confirming && staged && status.apply_available && (
        <div role="group" aria-label="Confirm installation">
          <p>
            The hub is checked and replaced by version {staged.version} and
            restarts. Agents reconnect by themselves. If the new version does
            not start, the previous one is put back.
          </p>
          <p className="row-actions">
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                setConfirming(false);
                void onApply();
              }}
            >
              Install now
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              Cancel
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
      <h3 id="update-settings-heading">Settings</h3>
      <form onSubmit={submit}>
        <fieldset>
          <legend>When a new version is out</legend>
          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="manual"
              checked={mode === "manual"}
              onChange={() => setMode("manual")}
            />
            Only tell me
          </label>
          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="download"
              checked={mode === "download"}
              onChange={() => setMode("download")}
            />
            Download and check it, then wait for me
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
            Download it and install it by itself, inside a maintenance window
          </label>
          {!status.apply_enabled && (
            <p className="muted">
              Installing is not set up on this hub (updates.apply and the
              regulus-update.path unit, which the installer enables).
            </p>
          )}
          {mode === "automatic" && (
            <p className="row-actions">
              <label>
                Window starts
                <input
                  type="time"
                  value={start}
                  onChange={(e) => setStart(e.target.value)}
                />
              </label>
              <label>
                Window ends
                <input
                  type="time"
                  value={end}
                  onChange={(e) => setEnd(e.target.value)}
                />
              </label>
            </p>
          )}
          {mode === "automatic" && (
            <p className="muted">
              Times are those of the hub's server. The hub restarts during the
              update, and a version that had to be rolled back is not tried
              again by itself.
            </p>
          )}
        </fieldset>
        <label>
          Channel
          <select
            value={channel}
            onChange={(e) => setChannel(e.target.value as UpdateChannel)}
          >
            <option value="stable">Stable</option>
            <option value="beta">Beta (pre-releases too)</option>
          </select>
        </label>
        <button type="submit" disabled={!changed || !valid}>
          Save
        </button>
      </form>
    </section>
  );
}

const AGENT_STATUS: Record<RolloutAgent["status"], string> = {
  pending: "waiting",
  offered: "fetching the update",
  installing: "installing, waiting for it to come back",
  updated: "updated",
  failed: "failed",
  skipped: "not tried",
};

// Servers shows the managed servers that run an older version than the hub and
// updates them to it, a few at a time, stopping at the first failure.
function Servers() {
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
      <h3 id="servers-heading">Servers</h3>
      {error && <p role="alert">{error}</p>}
      <p>
        The servers this hub manages are updated to the version the hub runs (
        <strong>{rollout.target || "unknown"}</strong>). They fetch it from the
        hub, so they need no internet access, and a server whose new version
        does not reconnect goes back to the previous one by itself.
      </p>

      {rollout.state === "running" && (
        <>
          <p role="status">
            Updating the servers, {rollout.wave_size ?? 1} at a time. It stops
            at the first failure.
          </p>
          <AgentRows agents={rollout.agents} label="Update progress" />
          <p className="row-actions">
            <button
              type="button"
              onClick={() => void run(stopRollout)}
              disabled={busy}
            >
              Stop
            </button>
          </p>
        </>
      )}

      {rollout.state !== "running" && (
        <>
          {behind.length === 0 ? (
            <p role="status">
              Every connected server runs {rollout.target}, or is not managed by
              an agent that can be updated.
            </p>
          ) : (
            <>
              <p role="status">
                {behind.length}{" "}
                {behind.length === 1 ? "server runs" : "servers run"} an older
                version.
              </p>
              <ul aria-label="Servers behind">
                {behind.map((a) => (
                  <li key={a.id}>
                    {a.hostname || a.id} runs {a.from}
                  </li>
                ))}
              </ul>
              <p className="row-actions">
                <label>
                  At a time
                  <select
                    value={wave}
                    onChange={(e) => setWave(Number(e.target.value))}
                  >
                    <option value={1}>1</option>
                    <option value={3}>3</option>
                    <option value={10}>10</option>
                    <option value={100}>All</option>
                  </select>
                </label>
                <button
                  type="button"
                  onClick={() => void run(() => startRollout(wave))}
                  disabled={busy}
                >
                  Update{" "}
                  {behind.length === 1
                    ? "this server"
                    : `${behind.length} servers`}{" "}
                  to {rollout.target}
                </button>
              </p>
            </>
          )}
          {finished && rollout.agents.length > 0 && (
            <>
              <p className="muted">
                Last update of the servers:{" "}
                {rollout.state === "done"
                  ? "done"
                  : rollout.state === "stopped"
                    ? "stopped"
                    : "stopped by a failure"}
                {rollout.finished_at
                  ? `, ${formatAgo(rollout.finished_at)}`
                  : ""}
                .
              </p>
              <AgentRows agents={rollout.agents} label="Last update" />
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
  return (
    <ul aria-label={label}>
      {agents.map((a) => (
        <li key={a.id}>
          {a.hostname || a.id} ({a.from}): {AGENT_STATUS[a.status]}
          {a.message && a.status === "failed" ? ` — ${a.message}` : ""}
        </li>
      ))}
    </ul>
  );
}
