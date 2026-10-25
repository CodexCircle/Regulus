import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { Agent } from "./api";

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

function mockAgents(body: Agent[], ok = true) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({ ok, status: ok ? 200 : 500, json: () => Promise.resolve(body) }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("App", () => {
  it("shows the product name", () => {
    mockAgents([]);
    render(<App />);
    expect(screen.getByRole("heading", { name: "Regulus" })).toBeInTheDocument();
  });

  it("lists the agents", async () => {
    mockAgents([agent]);
    render(<App />);
    expect(await screen.findByText("local")).toBeInTheDocument();
    expect(screen.getByText("srv1")).toBeInTheDocument();
    expect(screen.getByText("Ubuntu 24.04 amd64")).toBeInTheDocument();
    expect(screen.getByText("connected")).toBeInTheDocument();
    expect(screen.getByText("1h 1m")).toBeInTheDocument();
  });

  it("shows an empty state", async () => {
    mockAgents([]);
    render(<App />);
    expect(await screen.findByText("No agents connected.")).toBeInTheDocument();
  });

  it("shows an error when the request fails", async () => {
    mockAgents([], false);
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent("failed: 500");
  });
});
