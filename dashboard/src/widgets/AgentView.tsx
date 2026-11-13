import type { ReactNode } from "react";
import { formatAgo } from "../format";
import { useT } from "../i18n";
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
  const t = useT();
  const view = useLiveAgent(config.agentId);
  if (view.status === "loading") return <p>{t("servers.loading")}</p>;
  if (view.status === "empty") return <p>{t("servers.list.noneConnected")}</p>;

  const { agents, agent, stale, connection } = view;
  return (
    <div className="agent-view">
      {agents.length > 1 && !config.fixed && (
        <select
          aria-label={t("servers.fact.server")}
          value={agent.id}
          onChange={(e) => setConfig({ ...config, agentId: e.target.value })}
        >
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name || a.hostname ? `${a.name || a.hostname} (${a.id})` : a.id}
            </option>
          ))}
        </select>
      )}
      {stale && (
        <p role="status" className="note warn">
          {t("servers.widget.staleNote", { ago: formatAgo(new Date(lastSeen(agent)).toISOString()) })}
        </p>
      )}
      {connection === "offline" && (
        <p role="status" className="note warn">
          {t("servers.widget.connectionLost")}
        </p>
      )}
      {children(view)}
    </div>
  );
}

// Waiting is shown by widgets that need a metrics report when none arrived yet.
export function Waiting() {
  const t = useT();
  return <p>{t("servers.widget.waiting")}</p>;
}
