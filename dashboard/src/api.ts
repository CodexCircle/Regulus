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

// User as returned by the authentication endpoints (see internal/auth).
export interface User {
  username: string;
  role: "admin" | "viewer";
  created_at: string;
}

export interface Me {
  setup_required: boolean;
  user: User | null;
}

// ApiError is thrown for any non-2xx answer; message is the hub's explanation
// when it sent one.
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    method,
    signal,
    credentials: "same-origin",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let message = `${method} ${path} failed: ${res.status}`;
    try {
      const data = (await res.json()) as { error?: string };
      if (data.error) message = data.error;
    } catch {
      // keep the generic message
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const fetchAgents = (signal?: AbortSignal) => request<Agent[]>("GET", "/api/agents", undefined, signal);

export const fetchMe = (signal?: AbortSignal) => request<Me>("GET", "/api/auth/me", undefined, signal);

export const login = (username: string, password: string) =>
  request<User>("POST", "/api/auth/login", { username, password });

export const setupAdmin = (setupToken: string, username: string, password: string) =>
  request<User>("POST", "/api/auth/setup", { setup_token: setupToken, username, password });

export const logout = () => request<void>("POST", "/api/auth/logout", {});

export interface LayoutResponse {
  // Whatever the dashboard saved, or null if nothing yet.
  layout: unknown;
  updated_at: string | null;
}

export const fetchLayout = (signal?: AbortSignal) => request<LayoutResponse>("GET", "/api/layout", undefined, signal);

export const saveLayout = (layout: unknown) => request<void>("PUT", "/api/layout", { layout });
