import { useEffect, useState } from "react";
import { ApiError, fetchAgents, type Agent } from "./api";
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
}

export function AgentList({ refreshMs = REFRESH_MS, heading = true, onUnauthorized: onUnauthorizedProp }: Props) {
  const session = useSession();
  const onUnauthorized = onUnauthorizedProp ?? session?.expired;
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    const load = () => {
      fetchAgents(controller.signal).then(
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
  }, [refreshMs, onUnauthorized]);

  return (
    <section aria-labelledby="agents-heading">
      {heading ? <h2 id="agents-heading">Agents</h2> : <h2 id="agents-heading" className="visually-hidden">Agents</h2>}
      {state.kind === "loading" && <p>Loading…</p>}
      {state.kind === "error" && <p role="alert">Could not load agents: {state.message}</p>}
      {state.kind === "ready" && state.agents.length === 0 && <p>No agents connected.</p>}
      {state.kind === "ready" && state.agents.length > 0 && <AgentTable agents={state.agents} />}
    </section>
  );
}

function AgentTable({ agents }: { agents: Agent[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>Agent</th>
          <th>Host</th>
          <th>System</th>
          <th>Version</th>
          <th>Status</th>
          <th>Last heartbeat</th>
          <th>Uptime</th>
        </tr>
      </thead>
      <tbody>
        {agents.map((a) => (
          <tr key={a.id}>
            <td>{a.id}</td>
            <td>{a.hostname ?? "-"}</td>
            <td>{[a.os_version ?? a.os, a.arch].filter(Boolean).join(" ") || "-"}</td>
            <td>{a.version || "-"}</td>
            <td>{a.status}</td>
            <td>{formatAgo(a.last_heartbeat)}</td>
            <td>{a.uptime_seconds === null ? "-" : formatDuration(a.uptime_seconds)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
