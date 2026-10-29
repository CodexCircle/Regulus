import { useEffect, useState } from "react";
import { AgentList } from "./AgentList";
import { createEnrollment, enableRemoteAgents, fetchAgentListener, type AgentListener, type Enrollment } from "./api";
import { useCan } from "./session";
import { errorMessage } from "./useApiList";

// ServersPage lists the servers and, for those who may manage servers, adds new
// ones. While remote agents are off it says so first, and lets those who may
// manage servers turn them on, instead of offering a token that cannot work.
export function ServersPage() {
  const canManage = useCan("servers.manage");
  const [reload, setReload] = useState(0);
  // undefined while loading; null when the hub does not say (an older hub).
  const [listener, setListener] = useState<AgentListener | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    fetchAgentListener().then(
      (l) => live && setListener(l),
      () => live && setListener(null),
    );
    return () => {
      live = false;
    };
  }, []);
  const off = listener != null && !listener.enabled;
  return (
    <>
      <AgentList all reloadKey={reload} />
      {off && <RemoteAgentsOff listener={listener} canManage={canManage} onEnabled={setListener} />}
      {canManage && listener !== undefined && !off && <AddServer onAdded={() => setReload((n) => n + 1)} />}
    </>
  );
}

const DEFAULT_AGENT_PORT = 8443;

function RemoteAgentsOff({
  listener,
  canManage,
  onEnabled,
}: {
  listener: AgentListener;
  canManage: boolean;
  onEnabled: (l: AgentListener) => void;
}) {
  const [port, setPort] = useState(String(DEFAULT_AGENT_PORT));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const n = Number(port);
  const valid = Number.isInteger(n) && n >= 1024 && n <= 65535;

  const turnOn = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      onEnabled(await enableRemoteAgents(n));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="panel" aria-labelledby="remote-agents-heading">
      <h3 id="remote-agents-heading">Remote agents are off</h3>
      <p>Only this hub's own server is managed. To add other servers, the hub must listen for their agents on a port of its own.</p>
      {canManage && listener.editable ? (
        <form onSubmit={(e) => void turnOn(e)}>
          <label>
            Port for remote agents
            <input value={port} onChange={(e) => setPort(e.target.value.trim())} inputMode="numeric" autoComplete="off" />
          </label>
          {!valid && <p className="muted">Use a port from 1024 to 65535.</p>}
          {error && <p role="alert">{error}</p>}
          <button type="submit" disabled={busy || !valid}>
            Turn on remote agents
          </button>
          <p className="muted">
            The hub starts listening at once and keeps it after a restart. Open this port in the firewall towards the servers you want to manage.
          </p>
        </form>
      ) : (
        <p className="muted">Someone who manages servers can turn them on here.</p>
      )}
    </section>
  );
}

const RELEASES_URL = "https://github.com/CodexCircle/Regulus/releases";

const enrollOptions = (grant: Enrollment, hubHost: string) =>
  [`--hub ${hubHost}:${grant.agent_port}`, `--token ${grant.token}`, `--ca-fingerprint ${grant.ca_fingerprint}`].join(" ");

// installCommand installs Regulus on a clean Ubuntu or Debian server as an
// agent and enrolls it. It downloads the script first and then runs it (piping
// it into sh breaks as soon as "-s" is lost), and installs the version the hub
// runs, from that release, so the agent matches the hub even before a stable
// release exists. A development hub has no release: it falls back to the
// latest stable one.
export function installCommand(grant: Enrollment, hubHost: string): string {
  const v = grant.hub_version;
  const script = v ? `${RELEASES_URL}/download/v${v}/install.sh` : `${RELEASES_URL}/latest/download/install.sh`;
  const version = v ? `--version ${v} ` : "";
  return `curl -fsSLO ${script} && sh install.sh ${version}--role agent ${enrollOptions(grant, hubHost)}`;
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
            Run this on the server you want to add (Ubuntu or Debian, as a user that can use sudo). It installs Regulus{grant.hub_version ? ` ${grant.hub_version}, the version of this hub,` : ""} enrolls the server and starts the
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
