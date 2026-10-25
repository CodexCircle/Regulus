import { useState } from "react";
import { AgentList } from "./AgentList";
import { createEnrollment, type Enrollment } from "./api";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";

// ServersPage lists the servers and, for admins, adds new ones.
export function ServersPage() {
  const session = useSession();
  const [reload, setReload] = useState(0);
  return (
    <>
      <AgentList all reloadKey={reload} />
      {session?.user.role === "admin" && <AddServer onAdded={() => setReload((n) => n + 1)} />}
    </>
  );
}

// installCommand is what to run on the new server.
export function installCommand(grant: Enrollment, hubHost: string): string {
  return [
    "regulus agent",
    `--hub ${hubHost}:${grant.agent_port}`,
    `--token ${grant.token}`,
    `--ca-fingerprint ${grant.ca_fingerprint}`,
    "--data-dir /var/lib/regulus-agent",
  ].join(" ");
}

function AddServer({ onAdded }: { onAdded: () => void }) {
  const [name, setName] = useState("");
  const [grant, setGrant] = useState<Enrollment | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      setGrant(await createEnrollment(name.trim()));
      setCopied(false);
      setName("");
      onAdded();
    } catch (err) {
      setGrant(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const command = grant ? installCommand(grant, window.location.hostname) : "";
  return (
    <section className="panel" aria-labelledby="add-server-heading">
      <h3 id="add-server-heading">Add a server</h3>
      <form onSubmit={(e) => void create(e)}>
        <label>
          Name (optional)
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoComplete="off" />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={busy}>
          Create enrollment token
        </button>
      </form>
      {grant && (
        <div role="status" aria-label="Enrollment token">
          <p>
            Run this on the server you want to add, as a user that can write the data directory. The token works once and expires at{" "}
            {new Date(grant.expires_at).toLocaleString()}. It is not shown again.
          </p>
          <input readOnly value={command} aria-label="Command" onFocus={(e) => e.target.select()} />
          <p className="muted">
            Replace <code>{window.location.hostname}</code> if the server reaches the hub under another name. The fingerprint lets the server check that it is
            talking to this hub before it sends the token.
          </p>
          <p>
            <button
              type="button"
              onClick={() => {
                navigator.clipboard.writeText(command).then(
                  () => setCopied(true),
                  () => setCopied(false),
                );
              }}
            >
              {copied ? "Copied" : "Copy command"}
            </button>
          </p>
        </div>
      )}
    </section>
  );
}
