import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import type { DashboardLayout } from "./layout";
import { WidgetRegistry } from "./widgets/registry";

function note() {
  return <p>note body</p>;
}

function boom(): never {
  throw new Error("kaput");
}

function makeRegistry() {
  const r = new WidgetRegistry();
  r.register({ id: "note", title: "Note", description: "A note.", defaultSize: { w: 6, h: 2 }, inDefaultLayout: true, component: note });
  r.register({ id: "boom", title: "Boom", description: "Always fails.", defaultSize: { w: 6, h: 2 }, component: boom });
  return r;
}

interface Hub {
  saved: unknown[];
  stored: unknown;
  getStatus: number;
  putStatus: number;
}

function mockHub(init: Partial<Hub> = {}): Hub {
  const hub: Hub = { saved: [], stored: null, getStatus: 200, putStatus: 204, ...init };
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, req?: RequestInit) => {
      const reply = (status: number, data?: unknown) =>
        Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data) });
      if (path !== "/api/layout") return reply(404, { error: "not found" });
      if (req?.method === "PUT") {
        hub.saved.push((JSON.parse(req.body as string) as { layout: unknown }).layout);
        return reply(hub.putStatus);
      }
      if (hub.getStatus >= 400) return reply(hub.getStatus, { error: "boom" });
      return reply(200, { layout: hub.stored, updated_at: null });
    }),
  );
  return hub;
}

const stored: DashboardLayout = {
  version: 1,
  widgets: [
    { i: "n1", type: "note", x: 0, y: 0, w: 6, h: 2 },
    { i: "b1", type: "boom", x: 6, y: 0, w: 6, h: 2 },
    { i: "g1", type: "gone", x: 0, y: 2, w: 6, h: 2 },
  ],
};

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => undefined); // the error boundary logs
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Dashboard", () => {
  it("shows the default layout to a user who never saved one", async () => {
    mockHub();
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);
    expect(await screen.findByRole("article", { name: "Note" })).toBeInTheDocument();
    expect(screen.getByText("note body")).toBeInTheDocument();
    expect(screen.queryByRole("article", { name: "Boom" })).not.toBeInTheDocument();
  });

  it("shows the saved layout, isolates a failing widget and flags an unknown one", async () => {
    mockHub({ stored });
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);

    expect(await screen.findByText("note body")).toBeInTheDocument();
    expect(await screen.findByText(/This widget failed: kaput/)).toBeInTheDocument();
    expect(screen.getByRole("article", { name: "Unavailable widget" })).toHaveTextContent("gone");
  });

  it("falls back to the default layout when the stored one is invalid", async () => {
    mockHub({ stored: { version: 99 } });
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);
    expect(await screen.findByText("note body")).toBeInTheDocument();
  });

  it("adds a widget and saves the layout", async () => {
    const hub = mockHub({ stored: { version: 1, widgets: [] } });
    const user = userEvent.setup();
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);

    expect(await screen.findByText(/dashboard is empty/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Customize" }));
    await user.click(screen.getByRole("button", { name: "Add widget" }));
    expect(screen.getByRole("list", { name: "Available widgets" })).toHaveTextContent("Always fails.");
    await user.click(screen.getByRole("button", { name: "Add Note" }));

    expect(await screen.findByText("note body")).toBeInTheDocument();
    await waitFor(() => expect(hub.saved).toHaveLength(1));
    const saved = hub.saved[0] as DashboardLayout;
    expect(saved).toMatchObject({ version: 1, widgets: [{ type: "note", x: 0, y: 0, w: 6, h: 2 }] });
    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("removes a widget and saves", async () => {
    const hub = mockHub({ stored: { version: 1, widgets: [stored.widgets[0]] } });
    const user = userEvent.setup();
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);

    await screen.findByText("note body");
    expect(screen.queryByRole("button", { name: "Remove Note" })).not.toBeInTheDocument(); // only while customizing
    await user.click(screen.getByRole("button", { name: "Customize" }));
    await user.click(screen.getByRole("button", { name: "Remove Note" }));

    expect(await screen.findByText(/dashboard is empty/)).toBeInTheDocument();
    await waitFor(() => expect(hub.saved).toEqual([{ version: 1, widgets: [] }]));
  });

  it("resets to the default layout after a confirmation", async () => {
    const hub = mockHub({ stored: { version: 1, widgets: [] } });
    const user = userEvent.setup();
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);

    await screen.findByText(/dashboard is empty/);
    await user.click(screen.getByRole("button", { name: "Customize" }));
    await user.click(screen.getByRole("button", { name: "Reset to default" }));
    expect(hub.saved).toHaveLength(0); // nothing happens before the confirmation
    await user.click(screen.getByRole("button", { name: "Yes, reset" }));

    expect(await screen.findByText("note body")).toBeInTheDocument();
    await waitFor(() => expect(hub.saved).toHaveLength(1));
  });

  it("reports a layout that could not be saved", async () => {
    mockHub({ stored: { version: 1, widgets: [] }, putStatus: 500 });
    const user = userEvent.setup();
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);

    await screen.findByText(/dashboard is empty/);
    await user.click(screen.getByRole("button", { name: "Customize" }));
    await user.click(screen.getByRole("button", { name: "Add widget" }));
    await user.click(screen.getByRole("button", { name: "Add Note" }));
    expect(await screen.findByText("Could not save the layout")).toBeInTheDocument();
  });

  it("shows an error when the layout cannot be loaded", async () => {
    mockHub({ getStatus: 500 });
    render(<Dashboard registry={makeRegistry()} saveDelayMs={0} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load the dashboard");
  });
});
