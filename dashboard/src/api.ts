// Agent as returned by GET /api/agents (see internal/hub/api.go).
export interface Agent {
  id: string;
  version: string;
  status: "connected" | "stale";
  hostname?: string;
  os?: string;
  os_version?: string;
  kernel?: string;
  arch?: string;
  cpu_count?: number;
  memory_total_bytes?: number;
  connected_at: string;
  last_heartbeat: string | null;
  metrics_at: string | null;
  uptime_seconds: number | null;
}

export async function fetchAgents(signal?: AbortSignal): Promise<Agent[]> {
  const res = await fetch("/api/agents", { signal });
  if (!res.ok) {
    throw new Error(`GET /api/agents failed: ${res.status}`);
  }
  return (await res.json()) as Agent[];
}
