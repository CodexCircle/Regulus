import { useEffect, useState } from "react";
import { AgentList } from "./AgentList";
import { createEnrollment, enableRemoteAgents, fetchAgentListener, type AgentListener, type Enrollment } from "./api";
import { useT } from "./i18n";
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
  const t = useT();
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
      <h3 id="remote-agents-heading">{t("servers.remoteOff.title")}</h3>
      <p>{t("servers.remoteOff.intro")}</p>
      {canManage && listener.editable ? (
        <form onSubmit={(e) => void turnOn(e)}>
          <label>
            {t("servers.remoteOff.portLabel")}
            <input value={port} onChange={(e) => setPort(e.target.value.trim())} inputMode="numeric" autoComplete="off" />
          </label>
          {!valid && <p className="muted">{t("servers.remoteOff.portHint")}</p>}
          {error && <p role="alert">{error}</p>}
          <button type="submit" disabled={busy || !valid}>
            {t("servers.remoteOff.turnOn")}
          </button>
          <p className="muted">{t("servers.remoteOff.turnOnNote")}</p>
        </form>
      ) : (
        <p className="muted">{t("servers.remoteOff.askAdmin")}</p>
      )}
    </section>
  );
}

const RELEASES_URL = "https://github.com/Stellatinus/Regulus/releases";

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
  const t = useT();
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
      <h3 id="add-server-heading">{t("servers.add.title")}</h3>
      <form onSubmit={(e) => void create(e)}>
        <label>
          {t("servers.add.nameLabel")}
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoComplete="off" />
        </label>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={busy}>
          {t("servers.add.createToken")}
        </button>
      </form>
      {grant && (
        <div role="status" aria-label={t("servers.add.tokenLabel")}>
          <p>
            {grant.hub_version
              ? t("servers.add.instructionsVersion", { version: grant.hub_version, expires: new Date(grant.expires_at).toLocaleString() })
              : t("servers.add.instructions", { expires: new Date(grant.expires_at).toLocaleString() })}
          </p>
          <CommandBox label={t("servers.add.installCommand")} copyLabel={t("servers.add.copyInstall")} command={installCommand(grant, host)} />
          <p className="muted">
            {t("servers.add.replaceBefore")} <code>{host}</code> {t("servers.add.replaceAfter")}
          </p>
          <p>{t("servers.add.alreadyInstalled")}</p>
          <CommandBox label={t("servers.add.agentCommand")} copyLabel={t("servers.add.copyAgent")} command={agentCommand(grant, host)} />
        </div>
      )}
    </section>
  );
}

function CommandBox({ label, copyLabel, command }: { label: string; copyLabel: string; command: string }) {
  const t = useT();
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
        {copied ? t("servers.add.copied") : copyLabel}
      </button>
    </p>
  );
}
