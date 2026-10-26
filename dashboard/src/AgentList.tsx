import { useEffect, useState } from "react";
import { ApiError, fetchAgents, type Agent } from "./api";
import { serverHref } from "./route";
import { formatAgo, formatDuration } from "./format";
import { useSession } from "./session";

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
      {heading ? <h2 id="agents-heading">Agents</h2> : <h2 id="agents-heading" className="visually-hidden">Agents</h2>}
      {state.kind === "loading" && <p>Loading…</p>}
      {state.kind === "error" && <p role="alert">Could not load agents: {state.message}</p>}
      {state.kind === "ready" && state.agents.length === 0 && <p>{all ? "No servers yet." : "No agents connected."}</p>}
      {state.kind === "ready" && state.agents.length > 0 && <AgentTable agents={state.agents} />}
    </section>
  );
}

// displayName is what to call a server: the name an admin gave it, else its
// host name, else its id.
export const displayName = (a: Agent) => a.name || a.hostname || a.id;

const STATUS_LABEL: Record<Agent["status"], string> = {
  connected: "online",
  stale: "stale",
  offline: "offline",
  revoked: "revoked",
  incompatible: "incompatible",
};

// versionNote says, in a few words, what to do about an agent whose release
// differs from the hub's, or "" when there is nothing to do.
export function versionNote(a: Agent): string {
  if (a.status === "incompatible") return "too old for this hub: update it";
  if (a.compatibility === "behind") return "behind the hub";
  if (a.compatibility === "ahead") return "ahead of the hub: update the hub";
  return "";
}

export const statusLabel = (a: Agent) => STATUS_LABEL[a.status];

// lastContact says when the hub last heard from a server.
export function lastContact(a: Agent): string {
  if (a.status === "connected" || a.status === "stale") return formatAgo(a.last_heartbeat);
  return a.last_seen_at ? formatAgo(a.last_seen_at) : "never connected";
}

function AgentTable({ agents }: { agents: Agent[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>Server</th>
          <th>System</th>
          <th>Version</th>
          <th>Status</th>
          <th>Last contact</th>
          <th>Uptime</th>
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
              {versionNote(a) && <span className="muted"> {versionNote(a)}</span>}
            </td>
            <td>
              <span className={`status ${a.status}`}>{statusLabel(a)}</span>
            </td>
            <td>{lastContact(a)}</td>
            <td>{a.uptime_seconds === null ? "-" : formatDuration(a.uptime_seconds)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
