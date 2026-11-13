import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import type { Agent, User } from "./api";
import { LiveProvider } from "./LiveProvider";
import { ServersPage } from "./ServersPage";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS } from "./test-users";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", permissions: ADMIN_PERMISSIONS, created_at: now, disabled: false, two_factor: false };

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
const revoked: Agent = { ...online, id: "old", name: "Old", status: "revoked", last_heartbeat: null, uptime_seconds: null };

// mockHub answers the few paths these pages ask for.
function mockHub(agents: Agent[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string) => {
      const reply = (status: number, data?: unknown) => Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data) });
      if (path === "/api/agents?all=true") return reply(200, agents);
      if (path === "/api/agents/listener") return reply(200, { enabled: true, editable: true });
      if (path === "/api/layout") return reply(200, { layout: { version: 1, widgets: [] }, updated_at: null });
      return reply(404, { error: "not found" });
    }),
  );
}

function renderAdmin(page: React.ReactNode) {
  return render(
    <SessionContext.Provider value={{ user: admin, signOut: () => Promise.resolve(), expired: vi.fn() }}>
      <LiveProvider>{page}</LiveProvider>
    </SessionContext.Provider>,
  );
}

beforeEach(() => {
  window.localStorage.setItem("regulus.language", "it");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("the servers pages in Italian", () => {
  it("shows the list of servers", async () => {
    mockHub([online, revoked]);
    renderAdmin(<ServersPage />);

    expect(await screen.findByRole("heading", { name: "Agenti" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Ultimo contatto" })).toBeInTheDocument();
    expect(screen.getByText("online")).toBeInTheDocument();
    expect(screen.getByText("revocato")).toBeInTheDocument();
    expect(screen.getByText("mai connesso")).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Aggiungi un server" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Crea token di registrazione" })).toBeInTheDocument();
    expect(screen.queryByText("Last contact")).not.toBeInTheDocument();
  });

  it("shows the dashboard and its widget picker", async () => {
    mockHub([]);
    renderAdmin(<Dashboard />);

    expect(await screen.findByText("La dashboard è vuota. Scegli Personalizza, poi Aggiungi widget.")).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Personalizza" }));
    await user.click(screen.getByRole("button", { name: "Aggiungi widget" }));

    const picker = screen.getByRole("list", { name: "Widget disponibili" });
    expect(picker).toHaveTextContent("Uso dei dischi");
    expect(screen.getByRole("button", { name: "Aggiungi Rete" })).toBeInTheDocument();
  });
});
