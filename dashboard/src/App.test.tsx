import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { Agent, Me, User } from "./api";
import { FakeSocket } from "./fake-socket";

const admin: User = { username: "admin", role: "admin", created_at: new Date().toISOString(), disabled: false, two_factor: false };

const agent: Agent = {
  id: "local",
  version: "0.1.0",
  status: "connected",
  hostname: "srv1",
  os: "linux",
  os_version: "Ubuntu 24.04",
  arch: "amd64",
  connected_at: new Date().toISOString(),
  last_heartbeat: new Date().toISOString(),
  metrics_at: new Date().toISOString(),
  uptime_seconds: 3700,
};

interface Hub {
  me: Me;
  agents?: Agent[];
  agentsStatus?: number;
  loginStatus?: number;
  calls: { method: string; path: string; body: unknown }[];
}

// mockHub replaces fetch with a fake hub that answers the endpoints the
// dashboard uses.
function mockHub(hub: Omit<Hub, "calls">): Hub {
  const state: Hub = { ...hub, calls: [] };
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? (JSON.parse(init.body as string) as unknown) : undefined;
      state.calls.push({ method, path, body });
      const reply = (status: number, data?: unknown) =>
        Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data) });

      if (path === "/api/auth/me") return reply(200, state.me);
      if (path === "/api/auth/login") {
        if (state.loginStatus && state.loginStatus >= 400) return reply(state.loginStatus, { error: "invalid username or password" });
        state.me = { setup_required: false, user: admin };
        return reply(200, admin);
      }
      if (path === "/api/auth/setup") {
        state.me = { setup_required: false, user: admin };
        return reply(200, admin);
      }
      if (path === "/api/auth/logout") {
        state.me = { setup_required: false, user: null };
        return reply(204);
      }
      if (path.split("?")[0] === "/api/agents") return reply(state.agentsStatus ?? 200, state.agents ?? []);
      if (path === "/api/layout") return init?.method === "PUT" ? reply(204) : reply(200, { layout: null, updated_at: null });
      return reply(404, { error: "not found" });
    }),
  );
  return state;
}

// Most tests look at the servers page; the dashboard has its own tests.
beforeEach(() => {
  window.location.hash = "#/servers";
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

describe("App", () => {
  it("shows the product name", () => {
    mockHub({ me: { setup_required: false, user: null } });
    render(<App />);
    expect(screen.getByRole("heading", { name: "Regulus" })).toBeInTheDocument();
  });

  it("asks for a login before showing anything", async () => {
    const hub = mockHub({ me: { setup_required: false, user: null }, agents: [agent] });
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByText("Agents")).not.toBeInTheDocument();
    expect(hub.calls.some((c) => c.path === "/api/agents")).toBe(false);
  });

  it("logs in and lists the agents", async () => {
    const hub = mockHub({ me: { setup_required: false, user: null }, agents: [agent] });
    const user = userEvent.setup();
    render(<App />);

    await user.type(await screen.findByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("link", { name: "srv1" })).toHaveAttribute("href", "#/servers/local");
    expect(screen.getByText("Ubuntu 24.04 amd64")).toBeInTheDocument();
    expect(screen.getByText("online")).toBeInTheDocument();
    expect(screen.getByText("1h 1m")).toBeInTheDocument();
    expect(hub.calls.find((c) => c.path === "/api/auth/login")?.body).toEqual({
      username: "admin",
      password: "correct horse battery",
    });
  });

  it("shows the Users page to administrators only", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agents: [] });
    const first = render(<App />);
    expect(await screen.findByRole("link", { name: "Users" })).toBeInTheDocument();
    first.unmount();

    const viewer: User = { ...admin, username: "eve", role: "viewer" };
    mockHub({ me: { setup_required: false, user: viewer }, agents: [] });
    render(<App />);
    expect(await screen.findByRole("link", { name: /eve/ })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
  });

  it("shows the error of a failed login", async () => {
    mockHub({ me: { setup_required: false, user: null }, loginStatus: 401 });
    const user = userEvent.setup();
    render(<App />);

    await user.type(await screen.findByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "wrong password!");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("invalid username or password");
  });

  it("creates the admin on first run", async () => {
    const hub = mockHub({ me: { setup_required: true, user: null }, agents: [] });
    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Create the admin account" })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Setup token"), " tok123 ");
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText(/^Password/), "correct horse battery");
    await user.type(screen.getByLabelText("Repeat the password"), "something else entirely");
    await user.click(screen.getByRole("button", { name: "Create the admin" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("do not match");
    expect(hub.calls.some((c) => c.path === "/api/auth/setup")).toBe(false);

    await user.clear(screen.getByLabelText("Repeat the password"));
    await user.type(screen.getByLabelText("Repeat the password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Create the admin" }));

    expect(await screen.findByText("No servers yet.")).toBeInTheDocument();
    expect(hub.calls.find((c) => c.path === "/api/auth/setup")?.body).toEqual({
      setup_token: "tok123",
      username: "admin",
      password: "correct horse battery",
    });
  });

  it("goes straight to the agents when already signed in, and signs out", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agents: [] });
    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByText("No servers yet.")).toBeInTheDocument();
    expect(screen.getByText("admin")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("navigates between the dashboard and the servers page", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agents: [agent] });
    const user = userEvent.setup();
    window.location.hash = "";
    render(<App />);

    expect(await screen.findByRole("article", { name: "Servers" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Dashboard" })).toHaveAttribute("aria-current", "page");

    await user.click(screen.getByRole("link", { name: "Servers" }));
    expect(await screen.findByRole("heading", { name: "Agents" })).toBeInTheDocument();
    expect(screen.queryByRole("article", { name: "Servers" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Servers" })).toHaveAttribute("aria-current", "page");
  });

  it("fills the default dashboard with live data of the hub's server", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agents: [agent] });
    window.location.hash = "";
    render(<App />);

    for (const name of ["System", "CPU", "Memory", "Network", "Disk usage", "Servers"]) {
      expect(await screen.findByRole("article", { name })).toBeInTheDocument();
    }
    expect(FakeSocket.last.url).toMatch(/\/api\/stream$/);
    expect(await within(screen.getByRole("article", { name: "System" })).findByText("srv1 (local)")).toBeInTheDocument();
  });

  it("returns to the login page when the session ends", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agentsStatus: 401 });
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows an error when the agents cannot be loaded", async () => {
    mockHub({ me: { setup_required: false, user: admin }, agentsStatus: 500 });
    render(<App />);
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("500"));
  });
});
