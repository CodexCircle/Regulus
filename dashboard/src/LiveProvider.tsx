import { createContext, useContext, useEffect, useMemo, useReducer, useRef, useState, type ReactNode } from "react";
import { ApiError, fetchAgents, fetchMetrics, type Agent, type MetricsPoint } from "./api";
import { initialLiveState, isStale, liveReducer, parseStreamMessage, pickAgent, type LiveState } from "./live";
import { useSession } from "./session";

export type Connection = "connecting" | "live" | "offline";

// The part of WebSocket that the provider uses, so tests can supply a fake.
export interface SocketLike {
  onopen: (() => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  close(): void;
}

export type SocketFactory = (url: string) => SocketLike;

const browserSocket: SocketFactory = (url) => new WebSocket(url) as unknown as SocketLike;

const MAX_RETRY_MS = 15000;
const CLOCK_MS = 5000;

interface LiveValue {
  state: LiveState;
  connection: Connection;
  // The current time, refreshed every few seconds, for staleness checks.
  now: number;
}

const LiveContext = createContext<LiveValue | null>(null);

interface Props {
  children: ReactNode;
  createSocket?: SocketFactory;
  // First wait before reconnecting; it doubles after each failure.
  retryMs?: number;
}

// LiveProvider keeps the agents and their recent metrics up to date for the
// widgets: it loads them over HTTP, then follows /api/stream. If the stream
// drops it reconnects with a growing delay and fills the gap from the history.
export function LiveProvider({ children, createSocket = browserSocket, retryMs = 1000 }: Props) {
  const session = useSession();
  const [state, dispatch] = useReducer(liveReducer, initialLiveState);
  const [connection, setConnection] = useState<Connection>("connecting");
  const [generation, setGeneration] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  const expired = useRef(session?.expired);
  const latest = useRef(state);
  useEffect(() => {
    expired.current = session?.expired;
    latest.current = state;
  });

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    let stopped = false;
    let socket: SocketLike | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    const controller = new AbortController();

    const retry = () => {
      if (stopped) return;
      setConnection("offline");
      const delay = Math.min(retryMs * 2 ** failures, MAX_RETRY_MS);
      failures++;
      timer = setTimeout(() => {
        setConnection("connecting");
        connect();
      }, delay);
    };

    const openSocket = () => {
      const scheme = window.location.protocol === "https:" ? "wss" : "ws";
      const ws = createSocket(`${scheme}://${window.location.host}/api/stream`);
      socket = ws;
      ws.onopen = () => {
        failures = 0;
        setConnection("live");
        setGeneration((g) => g + 1);
      };
      ws.onmessage = (event) => {
        const action = parseStreamMessage(event.data);
        if (action) dispatch(action);
      };
      ws.onerror = () => undefined; // a close always follows
      ws.onclose = () => {
        socket = null;
        retry();
      };
    };

    // Loading the agents over HTTP first shows data quickly and tells an
    // ended session (401) from a hub that is just unreachable.
    const connect = () => {
      fetchAgents(controller.signal).then(
        (agents: Agent[]) => {
          if (stopped) return;
          dispatch({ type: "agents", agents });
          openSocket();
        },
        (err: unknown) => {
          if (stopped) return;
          if (err instanceof ApiError && err.status === 401) {
            expired.current?.();
            return;
          }
          retry();
        },
      );
    };

    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller.abort();
      if (socket) {
        socket.onclose = null;
        socket.close();
      }
    };
  }, [createSocket, retryMs]);

  // Fetch the history of each agent: all of it the first time, afterwards only
  // what was missed while the stream was down.
  const requested = useRef(new Map<string, number>());
  const agentIds = state.agents.map((a) => a.id).join("\n");
  useEffect(() => {
    const ids = agentIds === "" ? [] : agentIds.split("\n");
    const controller = new AbortController();
    for (const id of [...requested.current.keys()]) {
      if (!ids.includes(id)) requested.current.delete(id);
    }
    for (const id of ids) {
      if (requested.current.get(id) === generation) continue;
      requested.current.set(id, generation);
      const last = latest.current.metrics[id]?.at(-1)?.at;
      fetchMetrics(id, last, controller.signal).then(
        (points: MetricsPoint[]) => dispatch({ type: "history", id, points }),
        (err: unknown) => {
          if (controller.signal.aborted) return;
          if (err instanceof ApiError && err.status === 401) expired.current?.();
          requested.current.delete(id); // try again at the next change
        },
      );
    }
    return () => controller.abort();
  }, [agentIds, generation]);

  const value = useMemo(() => ({ state, connection, now }), [state, connection, now]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

// LiveAgent is what a widget shows: one agent and its recent reports.
export interface LiveAgent {
  agents: Agent[];
  agent: Agent;
  stale: boolean;
  // Recent reports, oldest first, and the newest of them (if any yet).
  points: MetricsPoint[];
  latest: MetricsPoint | undefined;
}

export type LiveView =
  | { status: "loading" }
  | { status: "empty" }
  | ({ status: "ready"; connection: Connection } & LiveAgent);

// useLiveAgent returns the agent a widget should show: the one named in its
// settings if connected, otherwise the hub's own or the first.
export function useLiveAgent(preferred: unknown): LiveView {
  const live = useContext(LiveContext);
  if (!live || !live.state.ready) return { status: "loading" };
  const { state, connection, now } = live;
  const agent = pickAgent(state.agents, preferred);
  if (!agent) return { status: "empty" };
  const points = state.metrics[agent.id] ?? [];
  return {
    status: "ready",
    connection,
    agents: state.agents,
    agent,
    stale: isStale(agent, state.staleAfterMs, now),
    points,
    latest: points.at(-1),
  };
}
