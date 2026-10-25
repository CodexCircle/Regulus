import type { Agent, MetricsPoint } from "./api";

// How many reports are kept per agent; the hub keeps the same.
export const MAX_POINTS = 3600;

export interface LiveState {
  // Connected agents, ordered by id.
  agents: Agent[];
  // Recent reports of each agent, oldest first.
  metrics: Record<string, MetricsPoint[]>;
  // Silence after which the hub calls an agent stale; null until known.
  staleAfterMs: number | null;
  // Whether the list of agents was received at least once.
  ready: boolean;
}

export type LiveAction =
  | { type: "agents"; agents: Agent[]; staleAfterSeconds?: number }
  | { type: "connected"; agent: Agent }
  | { type: "disconnected"; id: string }
  | { type: "heartbeat"; id: string; at: string }
  | { type: "metrics"; id: string; point: MetricsPoint }
  | { type: "history"; id: string; points: MetricsPoint[] };

export const initialLiveState: LiveState = { agents: [], metrics: {}, staleAfterMs: null, ready: false };

const byId = (a: Agent, b: Agent) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

// mergePoints adds points to a series ordered by time, dropping repeats (a
// report can arrive both from the history and from the stream) and the oldest
// ones beyond MAX_POINTS.
export function mergePoints(series: MetricsPoint[], incoming: MetricsPoint[]): MetricsPoint[] {
  if (incoming.length === 0) return series;
  const last = series.length > 0 ? Date.parse(series[series.length - 1].at) : -Infinity;
  if (incoming.length === 1 && Date.parse(incoming[0].at) > last) {
    const next = [...series, incoming[0]];
    return next.length > MAX_POINTS ? next.slice(next.length - MAX_POINTS) : next;
  }
  const seen = new Set<number>();
  const merged = [...series, ...incoming]
    .map((p) => ({ p, t: Date.parse(p.at) }))
    .filter(({ t }) => !Number.isNaN(t) && !seen.has(t) && seen.add(t))
    .sort((a, b) => a.t - b.t)
    .map(({ p }) => p);
  return merged.length > MAX_POINTS ? merged.slice(merged.length - MAX_POINTS) : merged;
}

function upsert(agents: Agent[], agent: Agent): Agent[] {
  return [...agents.filter((a) => a.id !== agent.id), agent].sort(byId);
}

function withAgent(state: LiveState, id: string, change: (a: Agent) => Agent): LiveState {
  if (!state.agents.some((a) => a.id === id)) return state;
  return { ...state, agents: state.agents.map((a) => (a.id === id ? change(a) : a)) };
}

export function liveReducer(state: LiveState, action: LiveAction): LiveState {
  switch (action.type) {
    case "agents": {
      const agents = [...action.agents].sort(byId);
      const ids = new Set(agents.map((a) => a.id));
      const metrics = Object.fromEntries(Object.entries(state.metrics).filter(([id]) => ids.has(id)));
      const staleAfterMs = action.staleAfterSeconds === undefined ? state.staleAfterMs : action.staleAfterSeconds * 1000;
      return { agents, metrics, staleAfterMs, ready: true };
    }
    case "connected":
      return { ...state, agents: upsert(state.agents, action.agent), ready: true };
    case "disconnected": {
      const metrics = Object.fromEntries(Object.entries(state.metrics).filter(([id]) => id !== action.id));
      return { ...state, agents: state.agents.filter((a) => a.id !== action.id), metrics };
    }
    case "heartbeat":
      return withAgent(state, action.id, (a) => ({ ...a, status: "connected", last_heartbeat: action.at }));
    case "metrics": {
      const { id, point } = action;
      const next = withAgent(state, id, (a) => ({
        ...a,
        status: "connected",
        metrics_at: point.at,
        uptime_seconds: point.uptime_seconds,
      }));
      if (next === state) return state;
      return { ...next, metrics: { ...next.metrics, [id]: mergePoints(next.metrics[id] ?? [], [point]) } };
    }
    case "history": {
      if (!state.agents.some((a) => a.id === action.id)) return state;
      return { ...state, metrics: { ...state.metrics, [action.id]: mergePoints(state.metrics[action.id] ?? [], action.points) } };
    }
  }
}

// parseStreamMessage turns a message of GET /api/stream into an action, or
// null for anything it does not understand (so new event types are ignored).
export function parseStreamMessage(data: unknown): LiveAction | null {
  if (typeof data !== "string") return null;
  let msg: Record<string, unknown>;
  try {
    const parsed: unknown = JSON.parse(data);
    if (typeof parsed !== "object" || parsed === null) return null;
    msg = parsed as Record<string, unknown>;
  } catch {
    return null;
  }
  const str = (v: unknown): v is string => typeof v === "string" && v !== "";
  switch (msg.type) {
    case "snapshot":
      return Array.isArray(msg.agents)
        ? {
            type: "agents",
            agents: msg.agents as Agent[],
            staleAfterSeconds: typeof msg.stale_after_seconds === "number" ? msg.stale_after_seconds : undefined,
          }
        : null;
    case "agent.connected":
      return typeof msg.agent === "object" && msg.agent !== null ? { type: "connected", agent: msg.agent as Agent } : null;
    case "agent.disconnected":
      return str(msg.id) ? { type: "disconnected", id: msg.id } : null;
    case "agent.heartbeat":
      return str(msg.id) && str(msg.at) ? { type: "heartbeat", id: msg.id, at: msg.at } : null;
    case "agent.metrics":
      return str(msg.id) && typeof msg.point === "object" && msg.point !== null
        ? { type: "metrics", id: msg.id, point: msg.point as MetricsPoint }
        : null;
    default:
      return null;
  }
}

// lastSeen is when the hub last heard from the agent, in epoch milliseconds.
export function lastSeen(agent: Agent): number {
  return Math.max(...[agent.last_heartbeat, agent.metrics_at, agent.connected_at].map((t) => (t ? Date.parse(t) : 0)));
}

// isStale tells whether the agent has been silent for longer than the hub's
// staleness limit. The hub's own flag only changes when a message arrives, so
// the dashboard also checks as time passes.
export function isStale(agent: Agent, staleAfterMs: number | null, now: number): boolean {
  if (staleAfterMs !== null && now - lastSeen(agent) > staleAfterMs) return true;
  return agent.status === "stale" && staleAfterMs === null;
}

// pickAgent chooses the agent a widget shows: the one saved in its settings if
// it is connected, else the hub's own built-in agent, else the first one.
export function pickAgent(agents: Agent[], preferred: unknown): Agent | undefined {
  return (
    agents.find((a) => a.id === preferred) ?? agents.find((a) => a.id === "local") ?? agents[0]
  );
}
