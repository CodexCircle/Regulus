import { useCallback, useEffect, useState } from "react";
import { displayName, lastContact, statusLabel, versionNote } from "./AgentList";
import { ApiError, can, fetchAgent, renameAgent, revokeAgent, type Agent } from "./api";
import { serverIdFromHash } from "./route";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";
import { useT } from "./i18n";
import { registry } from "./widgets";
import { widgetTitle } from "./widgets/registry";

// The widgets of the page of a server, in order.
const WIDGETS = ["system", "cpu", "memory", "network", "disk"];

const REFRESH_MS = 5000;

type State = { kind: "loading" } | { kind: "missing" } | { kind: "error"; message: string } | { kind: "ready"; agent: Agent };

// ServerPage shows one server: its state and the widgets bound to it.
export function ServerPage({ id = serverIdFromHash(window.location.hash) }: { id?: string }) {
  const t = useT();
  const session = useSession();
  const expired = session?.expired;
  const [state, setState] = useState<State>({ kind: "loading" });
  const [round, setRound] = useState(0);
  const reload = useCallback(() => setRound((n) => n + 1), []);

  useEffect(() => {
    const controller = new AbortController();
    const load = () =>
      fetchAgent(id, controller.signal).then(
        (agent) => setState({ kind: "ready", agent }),
        (err: unknown) => {
          if (controller.signal.aborted) return;
          if (err instanceof ApiError && err.status === 401) return expired?.();
          if (err instanceof ApiError && err.status === 404) return setState({ kind: "missing" });
          setState({ kind: "error", message: errorMessage(err) });
        },
      );
    void load();
    const timer = setInterval(() => void load(), REFRESH_MS);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [id, round, expired]);

  return (
    <section aria-labelledby="server-heading">
      <p>
        <a href="#/servers">{t("servers.server.back")}</a>
      </p>
      {state.kind === "loading" && <p>{t("servers.loading")}</p>}
      {state.kind === "error" && <p role="alert">{t("servers.server.loadError", { message: state.message })}</p>}
      {state.kind === "missing" && (
        <>
          <h2 id="server-heading">{t("servers.server.notFound")}</h2>
          <p>{t("servers.server.notFoundBody")}</p>
        </>
      )}
      {state.kind === "ready" && <Details agent={state.agent} canManage={can(session?.user, "servers.manage")} onChanged={reload} />}
    </section>
  );
}

function Details({ agent, canManage, onChanged }: { agent: Agent; canManage: boolean; onChanged: () => void }) {
  const t = useT();
  const online = agent.status === "connected" || agent.status === "stale";
  return (
    <>
      <h2 id="server-heading">
        {displayName(agent)} <span className={`status ${agent.status}`}>{statusLabel(agent, t)}</span>
      </h2>
      <dl className="facts server-facts">
        <div>
          <dt>{t("servers.fact.id")}</dt>
          <dd>{agent.id}</dd>
        </div>
        <div>
          <dt>{t("servers.fact.host")}</dt>
          <dd>{agent.hostname || "-"}</dd>
        </div>
        <div>
          <dt>{t("servers.fact.system")}</dt>
          <dd>{[agent.os_version ?? agent.os, agent.arch].filter(Boolean).join(" ") || "-"}</dd>
        </div>
        <div>
          <dt>{t("servers.fact.agentVersion")}</dt>
          <dd>
            {agent.version || "-"}
            {versionNote(agent, t) && <span className="muted"> ({versionNote(agent, t)})</span>}
          </dd>
        </div>
        <div>
          <dt>{t("servers.fact.lastContact")}</dt>
          <dd>{lastContact(agent, t)}</dd>
        </div>
      </dl>
      {canManage && agent.id !== "local" && <Manage agent={agent} onChanged={onChanged} />}
      {online ? (
        <div className="server-grid">
          {WIDGETS.map((w) => {
            const def = registry.get(w);
            if (!def) return null;
            const Widget = def.component;
            return (
              <section key={w} className="widget" aria-label={widgetTitle(def, t)}>
                <header>
                  <h3 className="widget-title">{widgetTitle(def, t)}</h3>
                </header>
                <div className="widget-body">
                  <Widget instanceId={`server-${w}`} config={{ agentId: agent.id, fixed: true }} setConfig={() => undefined} />
                </div>
              </section>
            );
          })}
        </div>
      ) : (
        <p role="status" className="note warn">
          {agent.status === "revoked"
            ? t("servers.server.revokedNote")
            : agent.status === "incompatible"
              ? t("servers.server.incompatibleNote", { reason: agent.incompatible_reason ?? t("servers.server.incompatibleFallback") })
              : t("servers.server.offlineNote")}
        </p>
      )}
    </>
  );
}

function Manage({ agent, onChanged }: { agent: Agent; onChanged: () => void }) {
  const t = useT();
  const [name, setName] = useState(agent.name ?? "");
  const [error, setError] = useState("");

  const run = async (action: () => Promise<unknown>) => {
    setError("");
    try {
      await action();
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="panel">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void run(() => renameAgent(agent.id, name.trim()));
        }}
      >
        <label>
          {t("servers.server.nameLabel")}
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} placeholder={agent.hostname || agent.id} />
        </label>
        <button type="submit">{t("servers.server.rename")}</button>
      </form>
      {error && <p role="alert">{error}</p>}
      {agent.status !== "revoked" && (
        <p>
          <button
            type="button"
            onClick={() => {
              if (window.confirm(t("servers.server.revokeConfirm", { name: displayName(agent) }))) {
                void run(() => revokeAgent(agent.id));
              }
            }}
          >
            {t("servers.server.revoke")}
          </button>
        </p>
      )}
    </div>
  );
}
