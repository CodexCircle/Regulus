import type { ReactNode } from "react";
import { formatAgo } from "../format";
import { lastSeen } from "../live";
import { useLiveAgent, type LiveAgent } from "../LiveProvider";
import type { WidgetProps } from "./registry";

interface Props extends WidgetProps {
  children: (live: LiveAgent) => ReactNode;
}

// AgentView is the frame shared by the widgets that show one server: it picks
// the agent (with a selector when there are several), handles loading, no
// agents, a stale agent and a lost connection, and draws the widget itself
// only when there is an agent to show.
export function AgentView({ config, setConfig, children }: Props) {
  const view = useLiveAgent(config.agentId);
  if (view.status === "loading") return <p>Loading…</p>;
  if (view.status === "empty") return <p>No agents connected.</p>;

  const { agents, agent, stale, connection } = view;
  return (
    <div className="agent-view">
      {agents.length > 1 && (
        <select
          aria-label="Server"
          value={agent.id}
          onChange={(e) => setConfig({ ...config, agentId: e.target.value })}
        >
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.hostname ? `${a.hostname} (${a.id})` : a.id}
            </option>
          ))}
        </select>
      )}
      {stale && (
        <p role="status" className="note warn">
          No news from this server since {formatAgo(new Date(lastSeen(agent)).toISOString())}.
        </p>
      )}
      {connection === "offline" && (
        <p role="status" className="note warn">
          Connection to the hub lost, reconnecting…
        </p>
      )}
      {children(view)}
    </div>
  );
}

// Waiting is shown by widgets that need a metrics report when none arrived yet.
export function Waiting() {
  return <p>Waiting for the first report…</p>;
}
