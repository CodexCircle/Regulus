import { useCallback, useEffect, useState } from "react";
import { displayName, lastContact, statusLabel } from "./AgentList";
import { ApiError, can, fetchAgent, renameAgent, revokeAgent, type Agent } from "./api";
import { serverIdFromHash } from "./route";
import { useSession } from "./session";
import { errorMessage } from "./useApiList";
import { registry } from "./widgets";

// The widgets of the page of a server, in order.
const WIDGETS = ["system", "cpu", "memory", "network", "disk"];

const REFRESH_MS = 5000;

type State = { kind: "loading" } | { kind: "missing" } | { kind: "error"; message: string } | { kind: "ready"; agent: Agent };

// ServerPage shows one server: its state and the widgets bound to it.
export function ServerPage({ id = serverIdFromHash(window.location.hash) }: { id?: string }) {
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
        <a href="#/servers">← All servers</a>
      </p>
      {state.kind === "loading" && <p>Loading…</p>}
      {state.kind === "error" && <p role="alert">Could not load the server: {state.message}</p>}
      {state.kind === "missing" && (
        <>
          <h2 id="server-heading">Server not found</h2>
          <p>There is no server with this id.</p>
        </>
      )}
      {state.kind === "ready" && <Details agent={state.agent} canManage={can(session?.user, "servers.manage")} onChanged={reload} />}
    </section>
  );
}

function Details({ agent, canManage, onChanged }: { agent: Agent; canManage: boolean; onChanged: () => void }) {
  const online = agent.status === "connected" || agent.status === "stale";
  return (
    <>
      <h2 id="server-heading">
        {displayName(agent)} <span className={`status ${agent.status}`}>{statusLabel(agent)}</span>
      </h2>
      <dl className="facts server-facts">
        <div>
          <dt>Id</dt>
          <dd>{agent.id}</dd>
        </div>
        <div>
          <dt>Host</dt>
          <dd>{agent.hostname || "-"}</dd>
        </div>
        <div>
          <dt>System</dt>
          <dd>{[agent.os_version ?? agent.os, agent.arch].filter(Boolean).join(" ") || "-"}</dd>
        </div>
        <div>
          <dt>Agent version</dt>
          <dd>{agent.version || "-"}</dd>
        </div>
        <div>
          <dt>Last contact</dt>
          <dd>{lastContact(agent)}</dd>
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
              <section key={w} className="widget" aria-label={def.title}>
                <header>
                  <h3 className="widget-title">{def.title}</h3>
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
            ? "This server was revoked and cannot connect any more. To use it again, remove its agent data directory and add it with a new token."
            : "This server is offline. Its metrics appear here again when its agent reconnects."}
        </p>
      )}
    </>
  );
}

function Manage({ agent, onChanged }: { agent: Agent; onChanged: () => void }) {
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
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} placeholder={agent.hostname || agent.id} />
        </label>
        <button type="submit">Rename</button>
      </form>
      {error && <p role="alert">{error}</p>}
      {agent.status !== "revoked" && (
        <p>
          <button
            type="button"
            onClick={() => {
              if (window.confirm(`Revoke ${displayName(agent)}? It will be disconnected and can never connect again.`)) {
                void run(() => revokeAgent(agent.id));
              }
            }}
          >
            Revoke this server
          </button>
        </p>
      )}
    </div>
  );
}
