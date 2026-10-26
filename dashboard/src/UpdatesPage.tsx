import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  checkForUpdates,
  downloadUpdate,
  fetchUpdates,
  saveUpdateSettings,
  type UpdateChannel,
  type UpdateMode,
  type UpdateStatus,
} from "./api";
import { formatAgo, formatBytes } from "./format";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

const POLL_MS = 2000;

// UpdatesPage shows the version of the hub, whether a newer release exists, and
// lets those with core.update check, download it and choose how the hub
// behaves. Applying an update is not here yet: the hub keeps the downloaded
// release, checked, until that exists.
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
  const working = status?.downloading || status?.checking;
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
          <Version status={status} busy={busy} onCheck={() => void run(checkForUpdates)} onDownload={() => void run(downloadUpdate)} />
          <Settings key={`${status.mode}/${status.channel}`} status={status} onSave={(mode, channel) => run(() => saveUpdateSettings(mode, channel))} />
        </>
      )}
    </section>
  );
}

function Version({ status, busy, onCheck, onDownload }: { status: UpdateStatus; busy: boolean; onCheck: () => void; onDownload: () => void }) {
  const { latest, staged } = status;
  const downloaded = staged !== null && latest !== null && staged.version === latest.version;
  return (
    <section className="panel" aria-labelledby="version-heading">
      <h3 id="version-heading">Version</h3>
      <p>
        This hub runs <strong>{status.current_version || "unknown"}</strong> ({status.platform}).
      </p>
      {!status.release_build && (
        <p className="muted">This is a development build. It does not compare itself with releases, so it is never offered an update.</p>
      )}
      {!status.checks_enabled && (
        <p role="status">Update checks are turned off in the configuration (<code>updates.check</code>), so the hub does not contact any server.</p>
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
            latest && <p role="status">Regulus is up to date: the newest {status.channel} release is {latest.version}.</p>
          )}
          {status.release_build && !latest && status.checked_at && !status.check_error && (
            <p role="status">The release server lists no {status.channel} release.</p>
          )}
          <p className="muted">{status.checked_at ? `Checked ${formatAgo(status.checked_at)}.` : "Not checked yet."}</p>
          {status.check_error && <p role="alert">The last check failed: {status.check_error}</p>}
        </>
      )}

      {staged && (
        <div role="status" aria-label="Downloaded update">
          <p>
            Version <strong>{staged.version}</strong> is downloaded ({formatBytes(staged.size)}, {staged.archive}). Its SHA-256 matches the release's checksum
            list. It is <strong>not installed</strong>: updating from the dashboard is not available yet.
          </p>
          <p className="muted">
            The checksum list comes from the same place as the file, so it catches a damaged download but does not prove who published it.
            {staged.signed ? " The release's signature file is kept, but is not checked yet." : " This release has no signature file."}
          </p>
        </div>
      )}
      {status.downloading && <p role="status">Downloading…</p>}
      {status.download_error && <p role="alert">The download failed: {status.download_error}</p>}

      <p className="row-actions">
        <button type="button" onClick={onCheck} disabled={busy || status.checking || !status.checks_enabled}>
          {status.checking ? "Checking…" : "Check now"}
        </button>
        {status.update_available && latest && !downloaded && (
          <button type="button" onClick={onDownload} disabled={busy || status.downloading}>
            Download {latest.version}
          </button>
        )}
      </p>
    </section>
  );
}

function Settings({ status, onSave }: { status: UpdateStatus; onSave: (mode: UpdateMode, channel: UpdateChannel) => Promise<void> }) {
  const [mode, setMode] = useState<UpdateMode>(status.mode);
  const [channel, setChannel] = useState<UpdateChannel>(status.channel);
  const changed = mode !== status.mode || channel !== status.channel;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void onSave(mode, channel);
  };

  return (
    <section className="panel" aria-labelledby="update-settings-heading">
      <h3 id="update-settings-heading">Settings</h3>
      <form onSubmit={submit}>
        <fieldset>
          <legend>When a new version is out</legend>
          <label className="choice">
            <input type="radio" name="mode" value="manual" checked={mode === "manual"} onChange={() => setMode("manual")} />
            Only tell me
          </label>
          <label className="choice">
            <input type="radio" name="mode" value="download" checked={mode === "download"} onChange={() => setMode("download")} />
            Download and check it, then wait for me
          </label>
          <p className="muted">Updating automatically, inside a maintenance window, with a rollback if something goes wrong, is not available yet.</p>
        </fieldset>
        <label>
          Channel
          <select value={channel} onChange={(e) => setChannel(e.target.value as UpdateChannel)}>
            <option value="stable">Stable</option>
            <option value="beta">Beta (pre-releases too)</option>
          </select>
        </label>
        <button type="submit" disabled={!changed}>
          Save
        </button>
      </form>
    </section>
  );
}
