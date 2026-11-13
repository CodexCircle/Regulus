import { useEffect, useState } from "react";
import { ApiError, fetchAgents, type Agent } from "./api";
import { serverHref } from "./route";
import { formatAgo, formatDuration } from "./format";
import { useSession } from "./session";
import { useT, type MessageKey } from "./i18n";
import type { Translator } from "./widgets/registry";

const REFRESH_MS = 5000;

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; agents: Agent[] };

interface Props {
  refreshMs?: number;
  // Show the "Agents" heading; a widget has its own title.
  heading?: boolean;
  // Called when the hub answers 401: the session has ended.
  onUnauthorized?: () => void;
  // List every enrolled server, also those that are offline or revoked.
  all?: boolean;
  // Called after the list changes; the page uses it to refresh when an agent is
  // added, renamed or revoked.
  reloadKey?: number;
}

export function AgentList({ refreshMs = REFRESH_MS, heading = true, onUnauthorized: onUnauthorizedProp, all = false, reloadKey = 0 }: Props) {
  const t = useT();
  const session = useSession();
  const onUnauthorized = onUnauthorizedProp ?? session?.expired;
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    const load = () => {
      fetchAgents(controller.signal, all).then(
        (agents) => setState({ kind: "ready", agents }),
        (err: unknown) => {
          if (controller.signal.aborted) return;
          if (err instanceof ApiError && err.status === 401) {
            onUnauthorized?.();
            return;
          }
          setState({ kind: "error", message: err instanceof Error ? err.message : String(err) });
        },
      );
    };
    load();
    const timer = setInterval(load, refreshMs);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [refreshMs, onUnauthorized, all, reloadKey]);

  return (
    <section aria-labelledby="agents-heading">
      {heading ? <h2 id="agents-heading">{t("servers.list.heading")}</h2> : <h2 id="agents-heading" className="visually-hidden">{t("servers.list.heading")}</h2>}
      {state.kind === "loading" && <p>{t("servers.loading")}</p>}
      {state.kind === "error" && <p role="alert">{t("servers.list.loadError", { message: state.message })}</p>}
      {state.kind === "ready" && state.agents.length === 0 && <p>{all ? t("servers.list.noServers") : t("servers.list.noneConnected")}</p>}
      {state.kind === "ready" && state.agents.length > 0 && <AgentTable agents={state.agents} />}
    </section>
  );
}

// displayName is what to call a server: the name an admin gave it, else its
// host name, else its id.
export const displayName = (a: Agent) => a.name || a.hostname || a.id;

const STATUS_LABEL: Record<Agent["status"], MessageKey> = {
  connected: "servers.status.online",
  stale: "servers.status.stale",
  offline: "servers.status.offline",
  revoked: "servers.status.revoked",
  incompatible: "servers.status.incompatible",
};

// versionNote says, in a few words, what to do about an agent whose release
// differs from the hub's, or "" when there is nothing to do.
export function versionNote(a: Agent, t: Translator): string {
  if (a.status === "incompatible") return t("servers.list.noteTooOld");
  if (a.compatibility === "behind") return t("servers.list.noteBehind");
  if (a.compatibility === "ahead") return t("servers.list.noteAhead");
  return "";
}

export const statusLabel = (a: Agent, t: Translator) => t(STATUS_LABEL[a.status]);

// lastContact says when the hub last heard from a server.
export function lastContact(a: Agent, t: Translator): string {
  if (a.status === "connected" || a.status === "stale") return formatAgo(a.last_heartbeat, undefined, t);
  return a.last_seen_at ? formatAgo(a.last_seen_at, undefined, t) : t("servers.list.neverConnected");
}

function AgentTable({ agents }: { agents: Agent[] }) {
  const t = useT();
  return (
    <table>
      <thead>
        <tr>
          <th>{t("servers.fact.server")}</th>
          <th>{t("servers.fact.system")}</th>
          <th>{t("servers.list.version")}</th>
          <th>{t("servers.fact.status")}</th>
          <th>{t("servers.fact.lastContact")}</th>
          <th>{t("servers.fact.uptime")}</th>
        </tr>
      </thead>
      <tbody>
        {agents.map((a) => (
          <tr key={a.id}>
            <td>
              <a href={serverHref(a.id)}>{displayName(a)}</a>
              {a.name && a.hostname && a.name !== a.hostname ? <span className="muted"> {a.hostname}</span> : null}
            </td>
            <td>{[a.os_version ?? a.os, a.arch].filter(Boolean).join(" ") || "-"}</td>
            <td>
              {a.version || "-"}
              {versionNote(a, t) && <span className="muted"> {versionNote(a, t)}</span>}
            </td>
            <td>
              <span className={`status ${a.status}`}>{statusLabel(a, t)}</span>
            </td>
            <td>{lastContact(a, t)}</td>
            <td>{a.uptime_seconds === null ? "-" : formatDuration(a.uptime_seconds, t)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
