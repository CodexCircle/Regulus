import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AccountPage } from "./AccountPage";
import { App } from "./App";
import type { AuditEntry, SessionInfo, User } from "./api";
import { ResetPage } from "./ResetPage";
import { SessionContext } from "./session";
import { UsersPage } from "./UsersPage";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", created_at: now, disabled: false, two_factor: false };
const viewer: User = { username: "eve", role: "viewer", created_at: now, disabled: false, two_factor: false };

interface Call {
  method: string;
  path: string;
  body: unknown;
}

// mockHub answers the account endpoints from handlers keyed by "METHOD path";
// status 4xx answers carry the error text.
function mockHub(given: Record<string, () => { status?: number; data?: unknown }>): Call[] {
  const calls: Call[] = [];
  // Parts of the pages these tests do not look at.
  const handlers: Record<string, () => { status?: number; data?: unknown }> = {
    "GET /api/settings/security": () => ({ data: { two_factor_policy: "off", two_factor_available: true } }),
    "GET /api/account/two-factor": () => ({ data: { enabled: false, mandatory: false, policy: "off", recovery_codes_left: 0 } }),
    ...given,
  };
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

function renderAs(user: User, page: React.ReactNode, expired = vi.fn()) {
  return render(
    <SessionContext.Provider value={{ user, signOut: () => Promise.resolve(), expired }}>{page}</SessionContext.Provider>,
  );
}

beforeEach(() => {
  vi.spyOn(window, "confirm").mockReturnValue(true);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("UsersPage", () => {
  it("is closed to viewers without asking the hub", () => {
    const calls = mockHub({});
    renderAs(viewer, <UsersPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Only administrators");
    expect(calls).toHaveLength(0);
  });

  it("lists accounts and activity, and protects the own account", async () => {
    const audit: AuditEntry[] = [{ id: 1, at: now, actor: "admin", event: "user_created", target: "eve", remote: "10.0.0.2", detail: "role viewer" }];
    mockHub({
      "GET /api/users": () => ({ data: [admin, viewer] }),
      "GET /api/audit?limit=100": () => ({ data: audit }),
    });
    renderAs(admin, <UsersPage />);

    expect(await screen.findByRole("combobox", { name: "Role of eve" })).toBeInTheDocument();
    expect(screen.getByText("admin (you)")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Role of admin" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Disable admin" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete admin" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete eve" })).toBeEnabled();
    expect(await screen.findByText("user created")).toBeInTheDocument();
    expect(screen.getByText("role viewer")).toBeInTheDocument();
  });

  it("creates a user and reloads the list", async () => {
    let users = [admin];
    const calls = mockHub({
      "GET /api/users": () => ({ data: users }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "POST /api/users": () => {
        users = [admin, viewer];
        return { status: 201, data: viewer };
      },
    });
    renderAs(admin, <UsersPage />);
    await screen.findByText("admin (you)");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), " eve ");
    await user.type(screen.getByLabelText("Password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Add user" }));

    expect(await screen.findByText("eve")).toBeInTheDocument();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ username: "eve", password: "correct horse battery", role: "viewer" });
    expect(screen.getByLabelText("Username")).toHaveValue("");
  });

  it("shows the hub's refusal when creating a user", async () => {
    mockHub({
      "GET /api/users": () => ({ data: [admin] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "POST /api/users": () => ({ status: 409, data: { error: "that username is taken" } }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByText("admin (you)");
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "eve");
    await user.type(screen.getByLabelText("Password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Add user" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("that username is taken");
  });

  it("changes a role, disables and deletes", async () => {
    const calls = mockHub({
      "GET /api/users": () => ({ data: [admin, viewer] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "PATCH /api/users/eve": () => ({ data: viewer }),
      "DELETE /api/users/eve": () => ({ status: 204 }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByText("eve");
    const user = userEvent.setup();

    await user.selectOptions(screen.getByRole("combobox", { name: "Role of eve" }), "admin");
    await waitFor(() => expect(calls).toContainEqual({ method: "PATCH", path: "/api/users/eve", body: { role: "admin" } }));

    await user.click(screen.getByRole("button", { name: "Disable eve" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "PATCH", path: "/api/users/eve", body: { disabled: true } }));

    await user.click(screen.getByRole("button", { name: "Delete eve" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "DELETE", path: "/api/users/eve", body: {} }));
  });

  it("does not delete when the confirmation is refused", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const calls = mockHub({
      "GET /api/users": () => ({ data: [admin, viewer] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByText("eve");
    await userEvent.setup().click(screen.getByRole("button", { name: "Delete eve" }));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
  });

  it("reports a refusal such as removing the last admin", async () => {
    mockHub({
      "GET /api/users": () => ({ data: [admin, viewer] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "PATCH /api/users/eve": () => ({ status: 409, data: { error: "there must be at least one active admin" } }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByText("eve");
    await userEvent.setup().click(screen.getByRole("button", { name: "Disable eve" }));
    expect(await screen.findByText("there must be at least one active admin")).toBeInTheDocument();
  });

  it("makes a reset link for a user and shows it once", async () => {
    const calls = mockHub({
      "GET /api/users": () => ({ data: [admin, viewer] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "POST /api/users/eve/reset": () => ({ status: 201, data: { username: "eve", token: "tok_123", expires_at: now } }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByRole("combobox", { name: "Role of eve" });
    expect(screen.getByRole("button", { name: "Reset password of admin" })).toBeDisabled();

    await userEvent.setup().click(screen.getByRole("button", { name: "Reset password of eve" }));
    const link = await screen.findByLabelText("Reset link");
    expect(link).toHaveValue(`${window.location.origin}${window.location.pathname}#/reset/tok_123`);
    expect(calls).toContainEqual({ method: "POST", path: "/api/users/eve/reset", body: {} });

    await userEvent.setup().click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Reset link")).not.toBeInTheDocument();
  });

  it("cannot reset a disabled account", async () => {
    mockHub({
      "GET /api/users": () => ({ data: [admin, { ...viewer, disabled: true }] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
    });
    renderAs(admin, <UsersPage />);
    await screen.findByRole("combobox", { name: "Role of eve" });
    expect(screen.getByRole("button", { name: "Reset password of eve" })).toBeDisabled();
  });

  it("returns to the login page when the session is gone", async () => {
    mockHub({
      "GET /api/users": () => ({ status: 401, data: { error: "unauthorized" } }),
      "GET /api/audit?limit=100": () => ({ status: 401, data: { error: "unauthorized" } }),
    });
    const expired = vi.fn();
    renderAs(admin, <UsersPage />, expired);
    await waitFor(() => expect(expired).toHaveBeenCalled());
  });
});

describe("AccountPage", () => {
  const sessions: SessionInfo[] = [
    { id: "aaa", remote_addr: "10.0.0.1", user_agent: "Firefox", created_at: now, last_used_at: now, expires_at: now, current: true },
    { id: "bbb", remote_addr: "10.0.0.9", user_agent: "Safari", created_at: now, last_used_at: now, expires_at: now, current: false },
  ];

  it("changes the password and clears the form", async () => {
    const calls = mockHub({
      "GET /api/account/sessions": () => ({ data: sessions }),
      "POST /api/account/password": () => ({ status: 204 }),
    });
    renderAs(viewer, <AccountPage />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "old password 1");
    await user.type(screen.getByLabelText("New password"), "new password 2");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByRole("status")).toHaveTextContent("Password changed");
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ current_password: "old password 1", new_password: "new password 2" });
    expect(screen.getByLabelText("Current password")).toHaveValue("");
  });

  it("shows a wrong current password", async () => {
    mockHub({
      "GET /api/account/sessions": () => ({ data: sessions }),
      "POST /api/account/password": () => ({ status: 403, data: { error: "the current password is wrong" } }),
    });
    renderAs(viewer, <AccountPage />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Current password"), "nope nope nope");
    await user.type(screen.getByLabelText("New password"), "new password 2");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("the current password is wrong");
  });

  it("lists sessions and ends another one, never the current one", async () => {
    let list = sessions;
    const calls = mockHub({
      "GET /api/account/sessions": () => ({ data: list }),
      "DELETE /api/account/sessions/bbb": () => {
        list = [sessions[0]];
        return { status: 204 };
      },
    });
    renderAs(viewer, <AccountPage />);
    const row = (await screen.findByText("Safari")).closest("tr")!;
    expect(screen.getByText("now (this session)")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /^Sign out/ })).toHaveLength(1);

    await userEvent.setup().click(within(row).getByRole("button", { name: "Sign out 10.0.0.9" }));
    await waitFor(() => expect(screen.queryByText("Safari")).not.toBeInTheDocument());
    expect(calls).toContainEqual({ method: "DELETE", path: "/api/account/sessions/bbb", body: {} });
  });
});

describe("ResetPage", () => {
  afterEach(() => {
    window.location.hash = "";
  });

  it("sets the new password with the token of the link", async () => {
    window.location.hash = "#/reset/tok_123";
    const calls = mockHub({ "POST /api/auth/reset": () => ({ status: 204 }) });
    render(<ResetPage />);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^New password/), "a brand new password");
    await user.type(screen.getByLabelText("Repeat the password"), "something different");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(screen.getByRole("alert")).toHaveTextContent("do not match");
    expect(calls).toHaveLength(0);

    await user.clear(screen.getByLabelText("Repeat the password"));
    await user.type(screen.getByLabelText("Repeat the password"), "a brand new password");
    await user.click(screen.getByRole("button", { name: "Change password" }));

    expect(await screen.findByRole("status")).toHaveTextContent("Your password was changed");
    expect(calls).toEqual([{ method: "POST", path: "/api/auth/reset", body: { token: "tok_123", new_password: "a brand new password" } }]);
    expect(window.location.hash).toBe(""); // the token is gone from the address
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows why a link does not work", async () => {
    window.location.hash = "#/reset/old";
    mockHub({ "POST /api/auth/reset": () => ({ status: 400, data: { error: "this reset link is invalid or has expired" } }) });
    render(<ResetPage />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/^New password/), "a brand new password");
    await user.type(screen.getByLabelText("Repeat the password"), "a brand new password");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("invalid or has expired");
  });

  it("opens without a login", async () => {
    window.location.hash = "#/reset/tok";
    const calls = mockHub({});
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Choose a new password" })).toBeInTheDocument();
    expect(calls).toHaveLength(0); // no /api/auth/me, no login page
  });
});
