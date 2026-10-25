import { describe, expect, it } from "vitest";
import type { Agent, MetricsPoint } from "./api";
import { initialLiveState, isStale, liveReducer, mergePoints, parseStreamMessage, pickAgent, MAX_POINTS, type LiveState } from "./live";

function agent(id: string, over: Partial<Agent> = {}): Agent {
  return {
    id,
    version: "1",
    status: "connected",
    connected_at: "2026-01-01T00:00:00Z",
    last_heartbeat: null,
    metrics_at: null,
    uptime_seconds: null,
    ...over,
  };
}

function point(at: string, over: Partial<MetricsPoint> = {}): MetricsPoint {
  return {
    at,
    cpu_percent: 1,
    load_1: 0,
    load_5: 0,
    load_15: 0,
    memory_total_bytes: 100,
    memory_used_bytes: 10,
    memory_available_bytes: 90,
    swap_total_bytes: 0,
    swap_used_bytes: 0,
    uptime_seconds: 5,
    disks: [],
    networks: [],
    ...over,
  };
}

const withAgents = (...ids: string[]): LiveState =>
  liveReducer(initialLiveState, { type: "agents", agents: ids.map((id) => agent(id)), staleAfterSeconds: 30 });

describe("liveReducer", () => {
  it("replaces the agents on a snapshot, sorted, and keeps the history of those still there", () => {
    let s = withAgents("b", "a");
    expect(s.agents.map((a) => a.id)).toEqual(["a", "b"]);
    expect(s.ready).toBe(true);
    expect(s.staleAfterMs).toBe(30000);

    s = liveReducer(s, { type: "history", id: "a", points: [point("2026-01-01T00:00:01Z")] });
    s = liveReducer(s, { type: "history", id: "b", points: [point("2026-01-01T00:00:01Z")] });
    s = liveReducer(s, { type: "agents", agents: [agent("b")] });
    expect(Object.keys(s.metrics)).toEqual(["b"]);
    expect(s.staleAfterMs).toBe(30000); // not sent this time: unchanged
  });

  it("adds, and removes with its metrics, an agent", () => {
    let s = withAgents("a");
    s = liveReducer(s, { type: "connected", agent: agent("0") });
    expect(s.agents.map((a) => a.id)).toEqual(["0", "a"]);
    s = liveReducer(s, { type: "metrics", id: "a", point: point("2026-01-01T00:00:01Z") });
    s = liveReducer(s, { type: "disconnected", id: "a" });
    expect(s.agents.map((a) => a.id)).toEqual(["0"]);
    expect(s.metrics).toEqual({});
  });

  it("a heartbeat revives a stale agent", () => {
    let s = liveReducer(initialLiveState, { type: "agents", agents: [agent("a", { status: "stale" })] });
    s = liveReducer(s, { type: "heartbeat", id: "a", at: "2026-01-01T00:00:09Z" });
    expect(s.agents[0]).toMatchObject({ status: "connected", last_heartbeat: "2026-01-01T00:00:09Z" });
  });

  it("appends metrics and updates the agent, ignoring unknown agents", () => {
    let s = withAgents("a");
    s = liveReducer(s, { type: "metrics", id: "a", point: point("2026-01-01T00:00:01Z", { uptime_seconds: 77 }) });
    expect(s.agents[0]).toMatchObject({ metrics_at: "2026-01-01T00:00:01Z", uptime_seconds: 77 });
    expect(s.metrics.a).toHaveLength(1);
    expect(liveReducer(s, { type: "metrics", id: "ghost", point: point("2026-01-01T00:00:02Z") })).toBe(s);
    expect(liveReducer(s, { type: "history", id: "ghost", points: [point("2026-01-01T00:00:02Z")] })).toBe(s);
  });
});

describe("mergePoints", () => {
  it("orders by time, drops repeats and caps the length", () => {
    const a = point("2026-01-01T00:00:01Z");
    const b = point("2026-01-01T00:00:02Z");
    const c = point("2026-01-01T00:00:03.5Z");
    expect(mergePoints([a, c], [b, a]).map((p) => p.at)).toEqual([a.at, b.at, c.at]);
    expect(mergePoints([a], [])).toEqual([a]);
    // 2026-01-01T00:00:03Z sorts after 00:00:03.5Z as text but not as a time
    expect(mergePoints([c], [point("2026-01-01T00:00:03Z")]).map((p) => p.at)).toEqual(["2026-01-01T00:00:03Z", c.at]);

    const many = Array.from({ length: MAX_POINTS + 5 }, (_, i) => point(new Date(Date.UTC(2026, 0, 1) + i * 1000).toISOString()));
    const capped = mergePoints([], many);
    expect(capped).toHaveLength(MAX_POINTS);
    expect(capped[0]).toBe(many[5]);
    expect(mergePoints(capped, [point(new Date(Date.UTC(2027, 0, 1)).toISOString())])).toHaveLength(MAX_POINTS);
  });
});

describe("parseStreamMessage", () => {
  const json = (v: unknown) => JSON.stringify(v);

  it("understands every event of the stream", () => {
    expect(parseStreamMessage(json({ type: "snapshot", stale_after_seconds: 30, agents: [] }))).toEqual({
      type: "agents",
      agents: [],
      staleAfterSeconds: 30,
    });
    expect(parseStreamMessage(json({ type: "agent.connected", agent: { id: "a" } }))).toMatchObject({ type: "connected" });
    expect(parseStreamMessage(json({ type: "agent.disconnected", id: "a" }))).toEqual({ type: "disconnected", id: "a" });
    expect(parseStreamMessage(json({ type: "agent.heartbeat", id: "a", at: "t" }))).toEqual({ type: "heartbeat", id: "a", at: "t" });
    expect(parseStreamMessage(json({ type: "agent.metrics", id: "a", point: {} }))).toMatchObject({ type: "metrics", id: "a" });
  });

  it("ignores what it does not understand", () => {
    for (const bad of [
      "not json",
      "null",
      "42",
      json({ type: "future.event" }),
      json({ type: "snapshot" }),
      json({ type: "agent.heartbeat", id: "a" }),
      json({ type: "agent.metrics", id: "a" }),
      json({ type: "agent.connected" }),
      new ArrayBuffer(1),
    ]) {
      expect(parseStreamMessage(bad)).toBeNull();
    }
  });
});

describe("isStale", () => {
  const now = Date.parse("2026-01-01T00:01:00Z");

  it("compares the last news with the hub's limit as time passes", () => {
    const a = agent("a", { last_heartbeat: "2026-01-01T00:00:50Z" });
    expect(isStale(a, 30000, now)).toBe(false);
    expect(isStale(a, 5000, now)).toBe(true);
    // metrics count as news too
    expect(isStale(agent("a", { last_heartbeat: "2026-01-01T00:00:00Z", metrics_at: "2026-01-01T00:00:55Z" }), 30000, now)).toBe(false);
  });

  it("trusts the hub's flag until the limit is known", () => {
    expect(isStale(agent("a", { status: "stale" }), null, now)).toBe(true);
    expect(isStale(agent("a"), null, now)).toBe(false);
  });
});

describe("pickAgent", () => {
  const agents = [agent("a"), agent("local"), agent("z")];

  it("prefers the saved one, then the built-in agent, then the first", () => {
    expect(pickAgent(agents, "z")?.id).toBe("z");
    expect(pickAgent(agents, "gone")?.id).toBe("local");
    expect(pickAgent(agents, 42)?.id).toBe("local");
    expect(pickAgent([agent("a"), agent("z")], undefined)?.id).toBe("a");
    expect(pickAgent([], "a")).toBeUndefined();
  });
});
