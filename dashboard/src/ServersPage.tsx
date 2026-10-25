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

const INSTALL_SCRIPT_URL = "https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh";

const enrollOptions = (grant: Enrollment, hubHost: string) =>
  [`--hub ${hubHost}:${grant.agent_port}`, `--token ${grant.token}`, `--ca-fingerprint ${grant.ca_fingerprint}`].join(" ");

// installCommand installs Regulus on a clean Ubuntu or Debian server as an
// agent and enrolls it, in one go.
export function installCommand(grant: Enrollment, hubHost: string): string {
  return `curl -fsSL ${INSTALL_SCRIPT_URL} | sh -s -- --role agent ${enrollOptions(grant, hubHost)}`;
}

// agentCommand enrolls and runs the agent on a server that already has the
// regulus binary.
export function agentCommand(grant: Enrollment, hubHost: string): string {
  return `regulus agent ${enrollOptions(grant, hubHost)} --data-dir /var/lib/regulus-agent`;
}

function AddServer({ onAdded }: { onAdded: () => void }) {
  const [name, setName] = useState("");
  const [grant, setGrant] = useState<Enrollment | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      setGrant(await createEnrollment(name.trim()));
      setName("");
      onAdded();
    } catch (err) {
      setGrant(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const host = window.location.hostname;
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
            Run this on the server you want to add (Ubuntu or Debian, as a user that can use sudo). It installs Regulus, enrolls the server and starts the
            agent. The token works once and expires at {new Date(grant.expires_at).toLocaleString()}. It is not shown again.
          </p>
          <CommandBox label="Install command" command={installCommand(grant, host)} />
          <p className="muted">
            Replace <code>{host}</code> if the server reaches the hub under another name. The fingerprint lets the server check that it is talking to this
            hub before it sends the token.
          </p>
          <p>Regulus already installed on that server? Enroll and run the agent with:</p>
          <CommandBox label="Agent command" command={agentCommand(grant, host)} />
        </div>
      )}
    </section>
  );
}

function CommandBox({ label, command }: { label: string; command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <p>
      <input readOnly value={command} aria-label={label} onFocus={(e) => e.target.select()} />{" "}
      <button
        type="button"
        onClick={() => {
          navigator.clipboard.writeText(command).then(
            () => setCopied(true),
            () => setCopied(false),
          );
        }}
      >
        {copied ? "Copied" : `Copy ${label.toLowerCase()}`}
      </button>
    </p>
  );
}
