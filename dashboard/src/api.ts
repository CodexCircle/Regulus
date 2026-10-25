// Agent as returned by GET /api/agents (see internal/hub/api.go).
export interface Agent {
  id: string;
  // The name an admin gave the server, if any.
  name?: string;
  version: string;
  // offline and revoked only come with fetchAgents(.., true): enrolled agents
  // that are not connected.
  status: "connected" | "stale" | "offline" | "revoked";
  hostname?: string;
  os?: string;
  os_version?: string;
  kernel?: string;
  arch?: string;
  cpu_count?: number;
  memory_total_bytes?: number;
  connected_at: string | null;
  last_seen_at?: string;
  last_heartbeat: string | null;
  metrics_at: string | null;
  uptime_seconds: number | null;
}

// User as returned by the authentication endpoints (see internal/auth).
export interface User {
  username: string;
  role: Role;
  created_at: string;
  disabled: boolean;
  // The user confirmed a second factor (TOTP).
  two_factor: boolean;
}

export type Role = "admin" | "viewer";

export interface Me {
  setup_required: boolean;
  user: User | null;
  // The hub requires two-factor authentication from this user, who has not set
  // it up: only the two-factor endpoints work until then.
  two_factor_setup_required?: boolean;
}

// ApiError is thrown for any non-2xx answer; message is the hub's explanation
// when it sent one.
export class ApiError extends Error {
  readonly status: number;
  // A machine-readable reason some answers carry, such as two_factor_setup_required.
  readonly code?: string;

  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// TWO_FACTOR_REQUIRED_EVENT is sent on window when the hub refuses a request
// because the user must set up two-factor authentication first.
export const TWO_FACTOR_REQUIRED_EVENT = "regulus:two-factor-required";

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
    let code: string | undefined;
    try {
      const data = (await res.json()) as { error?: string; code?: string };
      if (data.error) message = data.error;
      code = data.code;
    } catch {
      // keep the generic message
    }
    if (res.status === 403 && code === "two_factor_setup_required") {
      window.dispatchEvent(new Event(TWO_FACTOR_REQUIRED_EVENT));
    }
    throw new ApiError(res.status, message, code);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// fetchAgents lists the connected agents, or with all every enrolled agent.
export const fetchAgents = (signal?: AbortSignal, all = false) =>
  request<Agent[]>("GET", all ? "/api/agents?all=true" : "/api/agents", undefined, signal);

export const fetchAgent = (id: string, signal?: AbortSignal) =>
  request<Agent>("GET", `/api/agents/${encodeURIComponent(id)}`, undefined, signal);

// What an admin needs to add a server (see POST /api/enrollment-tokens).
export interface Enrollment {
  token: string;
  name: string;
  expires_at: string;
  ca_fingerprint: string;
  agent_port: number;
}

export const createEnrollment = (name: string) => request<Enrollment>("POST", "/api/enrollment-tokens", { name });

export const renameAgent = (id: string, name: string) =>
  request<void>("PATCH", `/api/agents/${encodeURIComponent(id)}`, { name });

export const revokeAgent = (id: string) => request<void>("DELETE", `/api/agents/${encodeURIComponent(id)}`, {});

export const fetchMe = (signal?: AbortSignal) => request<Me>("GET", "/api/auth/me", undefined, signal);

// The answer to a correct password when the account has two-factor on: no
// session yet, only a token to send with a code to loginTwoFactor.
export interface TwoFactorPending {
  two_factor_required: true;
  pending_token: string;
  expires_at: string;
}

export const isPending = (r: User | TwoFactorPending): r is TwoFactorPending => "two_factor_required" in r;

export const login = (username: string, password: string) =>
  request<User | TwoFactorPending>("POST", "/api/auth/login", { username, password });

// What the hub says after a login with a recovery code.
export type LoginResult = User & { recovery_code_used?: boolean; recovery_codes_left?: number };

// loginTwoFactor finishes a login with a code from the app or a recovery code.
export const loginTwoFactor = (pendingToken: string, code: string) =>
  request<LoginResult>("POST", "/api/auth/login/two-factor", { pending_token: pendingToken, code });

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

// One report of an agent as returned by GET /api/agents/{id}/metrics and
// pushed in "agent.metrics" stream events (see internal/hub/api.go).
export interface MetricsPoint {
  at: string;
  cpu_percent: number;
  load_1: number;
  load_5: number;
  load_15: number;
  memory_total_bytes: number;
  memory_used_bytes: number;
  memory_available_bytes: number;
  swap_total_bytes: number;
  swap_used_bytes: number;
  uptime_seconds: number;
  disks: DiskUsage[];
  networks: NetworkUsage[];
}

export interface DiskUsage {
  mount_point: string;
  filesystem: string;
  total_bytes: number;
  used_bytes: number;
}

export interface NetworkUsage {
  interface: string;
  rx_bytes: number;
  tx_bytes: number;
  // null until two reports allow computing a rate.
  rx_bytes_per_sec: number | null;
  tx_bytes_per_sec: number | null;
}

// fetchMetrics returns the recent metrics of an agent, oldest first; with
// since only the points received after that time.
export async function fetchMetrics(id: string, since?: string, signal?: AbortSignal): Promise<MetricsPoint[]> {
  const query = since ? `?since=${encodeURIComponent(since)}` : "";
  const res = await request<{ agent: string; points: MetricsPoint[] }>(
    "GET",
    `/api/agents/${encodeURIComponent(id)}/metrics${query}`,
    undefined,
    signal,
  );
  return res.points;
}

// One login session of the signed-in user (GET /api/account/sessions).
export interface SessionInfo {
  id: string;
  remote_addr: string;
  user_agent: string;
  created_at: string;
  last_used_at: string;
  expires_at: string;
  current: boolean;
}

// One account event of the audit log (GET /api/audit).
export interface AuditEntry {
  id: number;
  at: string;
  actor: string;
  event: string;
  target: string;
  remote: string;
  detail: string;
}

export const fetchUsers = (signal?: AbortSignal) => request<User[]>("GET", "/api/users", undefined, signal);

export const createUser = (username: string, password: string, role: Role) =>
  request<User>("POST", "/api/users", { username, password, role });

export const updateUser = (username: string, change: { role?: Role; disabled?: boolean }) =>
  request<User>("PATCH", `/api/users/${encodeURIComponent(username)}`, change);

export const deleteUser = (username: string) => request<void>("DELETE", `/api/users/${encodeURIComponent(username)}`, {});

export const fetchAudit = (limit = 100, signal?: AbortSignal) =>
  request<AuditEntry[]>("GET", `/api/audit?limit=${limit}`, undefined, signal);

export const changePassword = (currentPassword: string, newPassword: string) =>
  request<void>("POST", "/api/account/password", { current_password: currentPassword, new_password: newPassword });

export const fetchSessions = (signal?: AbortSignal) =>
  request<SessionInfo[]>("GET", "/api/account/sessions", undefined, signal);

export const revokeSession = (id: string) => request<void>("DELETE", `/api/account/sessions/${encodeURIComponent(id)}`, {});

export interface PasswordReset {
  username: string;
  token: string;
  expires_at: string;
}

// createReset makes a one-time password reset token for a user (admin only).
export const createReset = (username: string) =>
  request<PasswordReset>("POST", `/api/users/${encodeURIComponent(username)}/reset`, {});

// resetPassword chooses a new password with a reset token; it does not log in.
export const resetPassword = (token: string, newPassword: string) =>
  request<void>("POST", "/api/auth/reset", { token, new_password: newPassword });

// Two-factor authentication of the signed-in user (see internal/auth).
export interface TwoFactorStatus {
  enabled: boolean;
  // The hub requires two-factor from this user, so it cannot be turned off.
  mandatory: boolean;
  policy: TwoFactorPolicy;
  recovery_codes_left: number;
}

export type TwoFactorPolicy = "off" | "admins" | "all";

export interface TwoFactorSetup {
  secret: string;
  uri: string;
  // The QR code as a PNG data URL.
  qr: string;
}

export const fetchTwoFactor = (signal?: AbortSignal) =>
  request<TwoFactorStatus>("GET", "/api/account/two-factor", undefined, signal);

export const startTwoFactor = (password: string) =>
  request<TwoFactorSetup>("POST", "/api/account/two-factor/setup", { password });

export const enableTwoFactor = async (code: string) =>
  (await request<{ recovery_codes: string[] }>("POST", "/api/account/two-factor/enable", { code })).recovery_codes;

export const disableTwoFactor = (password: string, code: string) =>
  request<void>("POST", "/api/account/two-factor/disable", { password, code });

export const renewRecoveryCodes = async (password: string, code: string) =>
  (await request<{ recovery_codes: string[] }>("POST", "/api/account/two-factor/recovery-codes", { password, code })).recovery_codes;

export const resetUserTwoFactor = (username: string) =>
  request<void>("DELETE", `/api/users/${encodeURIComponent(username)}/two-factor`, {});

export interface SecuritySettings {
  two_factor_policy: TwoFactorPolicy;
  two_factor_available: boolean;
}

export const fetchSecurity = (signal?: AbortSignal) => request<SecuritySettings>("GET", "/api/settings/security", undefined, signal);

export const saveSecurity = (policy: TwoFactorPolicy) =>
  request<SecuritySettings>("PUT", "/api/settings/security", { two_factor_policy: policy });
