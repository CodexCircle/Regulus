import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, User } from "./api";
import { LiveProvider } from "./LiveProvider";
import { ServerPage } from "./ServerPage";
import { agentCommand, installCommand, ServersPage } from "./ServersPage";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS, VIEWER_PERMISSIONS } from "./test-users";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", permissions: ADMIN_PERMISSIONS, created_at: now, disabled: false, two_factor: false };
const viewer: User = { username: "eve", role: "viewer", permissions: VIEWER_PERMISSIONS, created_at: now, disabled: false, two_factor: false };

const online: Agent = {
  id: "web1",
  name: "Web",
  version: "0.1.0",
  status: "connected",
  hostname: "web1.lan",
  os: "linux",
  os_version: "Ubuntu 24.04",
  arch: "amd64",
  connected_at: now,
  last_heartbeat: now,
  metrics_at: now,
  uptime_seconds: 3700,
};

const offline: Agent = {
  id: "db1",
  version: "0.1.0",
  status: "offline",
  hostname: "db1.lan",
  connected_at: null,
  last_heartbeat: null,
  metrics_at: null,
  uptime_seconds: null,
  last_seen_at: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
};

const revoked: Agent = { ...offline, id: "old", hostname: "old.lan", status: "revoked" };

interface Call {
  method: string;
  path: string;
  body: unknown;
}

// mockHub answers from handlers keyed by "METHOD path".
function mockHub(handlers: Record<string, () => { status?: number; data?: unknown }>): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, path, body: init?.body ? (JSON.parse(init.body as string) as unknown) : undefined });
      const h = handlers[`${method} ${path}`];
      const { status = 200, data } = h ? h() : { status: 404, data: { error: "not found" } };
      return Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data) });
    }),
  );
  return calls;
}

function renderAs(user: User, page: React.ReactNode) {
  return render(
    <SessionContext.Provider value={{ user, signOut: () => Promise.resolve(), expired: vi.fn() }}>
      <LiveProvider>{page}</LiveProvider>
    </SessionContext.Provider>,
  );
}

beforeEach(() => {
  vi.spyOn(window, "confirm").mockReturnValue(true);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ServersPage", () => {
  it("lists every server with its state and links to its page", async () => {
    const calls = mockHub({ "GET /api/agents?all=true": () => ({ data: [online, offline, revoked] }) });
    renderAs(viewer, <ServersPage />);

    expect(await screen.findByRole("link", { name: "Web" })).toHaveAttribute("href", "#/servers/web1");
    expect(screen.getByRole("link", { name: "db1.lan" })).toHaveAttribute("href", "#/servers/db1");
    expect(screen.getByText("online")).toBeInTheDocument();
    expect(screen.getByText("offline")).toBeInTheDocument();
    expect(screen.getByText("revoked")).toBeInTheDocument();
    expect(screen.getAllByText(/^2h .*ago$/)).toHaveLength(2); // offline and revoked
    expect(calls[0].path).toBe("/api/agents?all=true");
  });

  it("tells which servers run another release than the hub", async () => {
    const behind: Agent = { ...online, id: "b", name: "Behind", compatibility: "behind", version: "0.2.1" };
    const ahead: Agent = { ...online, id: "a", name: "Ahead", compatibility: "ahead", version: "0.4.0" };
    const refused: Agent = { ...offline, id: "r", hostname: "r.lan", status: "incompatible", incompatible_reason: "agent version 0.1.0 is too far behind", version: "0.1.0" };
    mockHub({ "GET /api/agents?all=true": () => ({ data: [online, behind, ahead, refused] }) });
    renderAs(viewer, <ServersPage />);

    expect(await screen.findByText("behind the hub")).toBeInTheDocument();
    expect(screen.getByText("ahead of the hub: update the hub")).toBeInTheDocument();
    expect(screen.getByText("too old for this hub: update it")).toBeInTheDocument();
    expect(screen.getByText("incompatible")).toBeInTheDocument();
  });

  it("says when there is no server", async () => {
    mockHub({ "GET /api/agents?all=true": () => ({ data: [] }) });
    renderAs(viewer, <ServersPage />);
    expect(await screen.findByText("No servers yet.")).toBeInTheDocument();
  });

  it("offers adding a server to administrators only", async () => {
    mockHub({ "GET /api/agents?all=true": () => ({ data: [] }) });
    const first = renderAs(viewer, <ServersPage />);
    await screen.findByText("No servers yet.");
    expect(screen.queryByRole("heading", { name: "Add a server" })).not.toBeInTheDocument();
    first.unmount();

    renderAs(admin, <ServersPage />);
    expect(await screen.findByRole("heading", { name: "Add a server" })).toBeInTheDocument();
  });

  it("shows the install command once the token is created", async () => {
    const grant = {
      token: "tok123",
      name: "Web",
      expires_at: now,
      ca_fingerprint: "sha256:abcd",
      agent_port: 7443,
      hub_version: "0.1.0-alpha.1",
    };
    const calls = mockHub({
      "GET /api/agents?all=true": () => ({ data: [] }),
      "POST /api/enrollment-tokens": () => ({ data: grant }),
    });
    const user = userEvent.setup();
    renderAs(admin, <ServersPage />);

    await user.type(await screen.findByLabelText("Name (optional)"), " Web ");
    await user.click(screen.getByRole("button", { name: "Create enrollment token" }));

    const host = window.location.hostname;
    const options = `--hub ${host}:7443 --token tok123 --ca-fingerprint sha256:abcd`;
    const install = (await screen.findByLabelText("Install command")) as HTMLInputElement;
    expect(install.value).toBe(
      `curl -fsSLO https://github.com/CodexCircle/Regulus/releases/download/v0.1.0-alpha.1/install.sh && sh install.sh --version 0.1.0-alpha.1 --role agent ${options}`,
    );
    expect((screen.getByLabelText("Agent command") as HTMLInputElement).value).toBe(
      `regulus agent ${options} --data-dir /var/lib/regulus-agent`,
    );
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "Web" });
    // The list is fetched again so the new server shows up.
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/agents?all=true").length).toBeGreaterThan(1));
  });

  it("shows why a token could not be created", async () => {
    mockHub({
      "GET /api/agents?all=true": () => ({ data: [] }),
      "POST /api/enrollment-tokens": () => ({ status: 409, data: { error: "agent listener is off" } }),
    });
    const user = userEvent.setup();
    renderAs(admin, <ServersPage />);
    await user.click(await screen.findByRole("button", { name: "Create enrollment token" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("agent listener is off");
    expect(screen.queryByLabelText("Install command")).not.toBeInTheDocument();
  });

  it("builds the commands from the grant", () => {
    const grant = { token: "t", name: "", expires_at: now, ca_fingerprint: "sha256:f", agent_port: 1 };
    // A development hub has no release to point to: the latest stable one.
    expect(installCommand(grant, "hub.lan")).toBe(
      "curl -fsSLO https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh && sh install.sh --role agent --hub hub.lan:1 --token t --ca-fingerprint sha256:f",
    );
    // A release hub pins its own version, from its own release.
    expect(installCommand({ ...grant, hub_version: "0.2.0-beta.3" }, "hub.lan")).toBe(
      "curl -fsSLO https://github.com/CodexCircle/Regulus/releases/download/v0.2.0-beta.3/install.sh && sh install.sh --version 0.2.0-beta.3 --role agent --hub hub.lan:1 --token t --ca-fingerprint sha256:f",
    );
    expect(agentCommand(grant, "hub.lan")).toBe(
      "regulus agent --hub hub.lan:1 --token t --ca-fingerprint sha256:f --data-dir /var/lib/regulus-agent",
    );
  });
});

describe("ServerPage", () => {
  it("shows the facts and the widgets of an online server", async () => {
    mockHub({ "GET /api/agents/web1": () => ({ data: online }) });
    renderAs(viewer, <ServerPage id="web1" />);

    expect(await screen.findByRole("heading", { name: /Web/ })).toBeInTheDocument();
    expect(screen.getByText("web1.lan")).toBeInTheDocument();
    expect(screen.getByText("Ubuntu 24.04 amd64")).toBeInTheDocument();
    for (const title of ["System", "CPU", "Memory", "Network", "Disk usage"]) {
      expect(screen.getByRole("region", { name: title })).toBeInTheDocument();
    }
    // A viewer cannot manage the server.
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke this server" })).not.toBeInTheDocument();
  });

  it("explains an offline server instead of showing widgets", async () => {
    mockHub({ "GET /api/agents/db1": () => ({ data: offline }) });
    renderAs(viewer, <ServerPage id="db1" />);

    expect(await screen.findByRole("status")).toHaveTextContent("offline");
    expect(screen.queryByRole("region", { name: "CPU" })).not.toBeInTheDocument();
    expect(screen.getByText(/^2h .*ago$/)).toBeInTheDocument();
  });

  it("says a revoked server cannot come back", async () => {
    mockHub({ "GET /api/agents/old": () => ({ data: revoked }) });
    renderAs(admin, <ServerPage id="old" />);

    expect(await screen.findByRole("status")).toHaveTextContent("revoked");
    expect(screen.queryByRole("button", { name: "Revoke this server" })).not.toBeInTheDocument();
  });

  it("reports an unknown server", async () => {
    mockHub({});
    renderAs(viewer, <ServerPage id="nope" />);
    expect(await screen.findByRole("heading", { name: "Server not found" })).toBeInTheDocument();
  });

  it("lets an administrator rename a server", async () => {
    const calls = mockHub({
      "GET /api/agents/web1": () => ({ data: online }),
      "PATCH /api/agents/web1": () => ({ status: 204 }),
    });
    const user = userEvent.setup();
    renderAs(admin, <ServerPage id="web1" />);

    const input = await screen.findByLabelText("Name");
    await user.clear(input);
    await user.type(input, "Frontend");
    await user.click(screen.getByRole("button", { name: "Rename" }));

    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ name: "Frontend" }));
    // The page is fetched again after the change.
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/agents/web1" && c.method === "GET").length).toBeGreaterThan(1));
  });

  it("revokes a server after confirmation only", async () => {
    const calls = mockHub({
      "GET /api/agents/web1": () => ({ data: online }),
      "DELETE /api/agents/web1": () => ({ status: 204 }),
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const user = userEvent.setup();
    renderAs(admin, <ServerPage id="web1" />);

    await user.click(await screen.findByRole("button", { name: "Revoke this server" }));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);

    confirm.mockReturnValue(true);
    await user.click(screen.getByRole("button", { name: "Revoke this server" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
  });

  it("does not offer to manage the built-in agent", async () => {
    mockHub({ "GET /api/agents/local": () => ({ data: { ...online, id: "local", name: undefined } }) });
    renderAs(admin, <ServerPage id="local" />);
    await screen.findByRole("heading", { name: /web1\.lan/ });
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
  });
});
