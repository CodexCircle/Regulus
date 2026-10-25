import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { TwoFactorStatus, User } from "./api";
import { SessionContext } from "./session";
import { TwoFactor } from "./TwoFactor";
import { UsersPage } from "./UsersPage";
import { ADMIN_PERMISSIONS, VIEWER_PERMISSIONS } from "./test-users";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", permissions: ADMIN_PERMISSIONS, created_at: now, disabled: false, two_factor: false };

interface Call {
  method: string;
  path: string;
  body: unknown;
}

type Handler = (body: unknown) => { status?: number; data?: unknown };

// mockHub answers from handlers keyed by "METHOD path".
function mockHub(handlers: Record<string, Handler>): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? (JSON.parse(init.body as string) as unknown) : undefined;
      calls.push({ method, path, body });
      const h = handlers[`${method} ${path}`];
      const { status = 200, data } = h ? h(body) : { status: 404, data: { error: "not found" } };
      return Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data) });
    }),
  );
  return calls;
}

const status = (over: Partial<TwoFactorStatus> = {}): TwoFactorStatus => ({
  enabled: false,
  mandatory: false,
  policy: "off",
  recovery_codes_left: 0,
  ...over,
});

const setupInfo = { secret: "ABCD EFGH IJKL MNOP", uri: "otpauth://totp/Regulus:admin?secret=X", qr: "data:image/png;base64,AAAA" };

function renderTwoFactor(onChange?: () => void) {
  return render(
    <SessionContext.Provider value={{ user: admin, signOut: () => Promise.resolve(), expired: vi.fn() }}>
      <TwoFactor onChange={onChange} />
    </SessionContext.Provider>,
  );
}

beforeEach(() => {
  window.location.hash = "#/account";
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.location.hash = "";
});

describe("TwoFactor settings", () => {
  it("sets up with the password, then a code, then shows the recovery codes once", async () => {
    let on = false;
    const calls = mockHub({
      "GET /api/account/two-factor": () => ({
        data: on ? status({ enabled: true, recovery_codes_left: 10 }) : status(),
      }),
      "POST /api/account/two-factor/setup": () => ({ data: setupInfo }),
      "POST /api/account/two-factor/enable": () => {
        on = true;
        return { data: { recovery_codes: ["aaaaa-bbbbb", "ccccc-ddddd"] } };
      },
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    renderTwoFactor(onChange);

    await user.type(await screen.findByLabelText("Your password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Set up two-factor authentication" }));

    expect(await screen.findByAltText(/QR code/)).toHaveAttribute("src", setupInfo.qr);
    expect(screen.getByLabelText("Secret key")).toHaveTextContent("ABCD EFGH IJKL MNOP");
    expect(calls.find((c) => c.path.endsWith("/setup"))?.body).toEqual({ password: "correct horse battery" });

    await user.type(screen.getByLabelText(/Enter the 6-digit code/), "123 456");
    await user.click(screen.getByRole("button", { name: "Turn on two-factor authentication" }));

    const codes = await screen.findByRole("status", { name: "Recovery codes" });
    expect(codes).toHaveTextContent("aaaaa-bbbbb");
    expect(codes).toHaveTextContent("ccccc-ddddd");
    expect(calls.find((c) => c.path.endsWith("/enable"))?.body).toEqual({ code: "123 456" });

    // The codes stay until the user says they saved them.
    const done = screen.getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    expect(onChange).not.toHaveBeenCalled();
    await user.click(screen.getByLabelText(/I saved these codes/));
    await user.click(done);
    expect(onChange).toHaveBeenCalledOnce();
    expect(await screen.findByText(/Two-factor authentication is on/)).toBeInTheDocument();
    expect(screen.queryByText("aaaaa-bbbbb")).not.toBeInTheDocument();
  });

  it("shows why the setup was refused", async () => {
    mockHub({
      "GET /api/account/two-factor": () => ({ data: status() }),
      "POST /api/account/two-factor/setup": () => ({ status: 403, data: { error: "the current password is incorrect" } }),
    });
    const user = userEvent.setup();
    renderTwoFactor();
    await user.type(await screen.findByLabelText("Your password"), "nope nope nope");
    await user.click(screen.getByRole("button", { name: "Set up two-factor authentication" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("password is incorrect");
    expect(screen.queryByAltText(/QR code/)).not.toBeInTheDocument();
  });

  it("makes new recovery codes after the password and a code", async () => {
    const calls = mockHub({
      "GET /api/account/two-factor": () => ({ data: status({ enabled: true, recovery_codes_left: 7 }) }),
      "POST /api/account/two-factor/recovery-codes": () => ({ data: { recovery_codes: ["nnnnn-nnnnn"] } }),
    });
    const user = userEvent.setup();
    renderTwoFactor();

    expect(await screen.findByText("7")).toBeInTheDocument();
    expect(screen.queryByText(/running out/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Make new recovery codes" }));
    await user.type(screen.getByLabelText("Your password"), "correct horse battery");
    await user.type(screen.getByLabelText(/Code from the app/), "654321");
    await user.click(screen.getByRole("button", { name: "Make new recovery codes" }));

    expect(await screen.findByText("nnnnn-nnnnn")).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/recovery-codes"))?.body).toEqual({ password: "correct horse battery", code: "654321" });
  });

  it("warns when few recovery codes are left", async () => {
    mockHub({ "GET /api/account/two-factor": () => ({ data: status({ enabled: true, recovery_codes_left: 2 }) }) });
    renderTwoFactor();
    expect(await screen.findByText(/running out of recovery codes/)).toBeInTheDocument();
  });

  it("turns two-factor off with the password and a code", async () => {
    let on = true;
    const calls = mockHub({
      "GET /api/account/two-factor": () => ({ data: status({ enabled: on, recovery_codes_left: 10 }) }),
      "POST /api/account/two-factor/disable": () => {
        on = false;
        return { status: 204 };
      },
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    renderTwoFactor(onChange);

    await user.click(await screen.findByRole("button", { name: "Turn off two-factor authentication" }));
    await user.type(screen.getByLabelText("Your password"), "correct horse battery");
    await user.type(screen.getByLabelText(/Code from the app/), "111111");
    await user.click(screen.getByRole("button", { name: "Turn off" }));

    await waitFor(() => expect(onChange).toHaveBeenCalled());
    expect(calls.find((c) => c.path.endsWith("/disable"))?.body).toEqual({ password: "correct horse battery", code: "111111" });
    expect(await screen.findByRole("button", { name: "Set up two-factor authentication" })).toBeInTheDocument();
  });

  it("does not offer to turn off what the hub requires", async () => {
    mockHub({ "GET /api/account/two-factor": () => ({ data: status({ enabled: true, mandatory: true, policy: "all", recovery_codes_left: 10 }) }) });
    renderTwoFactor();
    expect(await screen.findByText(/cannot be turned off/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Turn off two-factor authentication" })).not.toBeInTheDocument();
  });
});

describe("Signing in with two-factor authentication", () => {
  // A hub whose admin has two-factor on.
  function hub(over: Record<string, Handler> = {}, extra: Record<string, unknown> = {}) {
    let signedIn = false;
    const calls = mockHub({
      "GET /api/auth/me": () => ({ data: { setup_required: false, user: signedIn ? { ...admin, two_factor: true } : null } }),
      "POST /api/auth/login": () => ({ data: { two_factor_required: true, pending_token: "pending-1", expires_at: now } }),
      "POST /api/auth/login/two-factor": (body) => {
        if ((body as { code: string }).code === "000000") return { status: 401, data: { error: "the code is not valid" } };
        signedIn = true;
        return { data: { ...admin, two_factor: true, ...extra } };
      },
      "GET /api/agents": () => ({ data: [] }),
      "GET /api/layout": () => ({ data: { layout: null, updated_at: null } }),
      ...over,
    });
    return calls;
  }

  async function passwordStep(user: ReturnType<typeof userEvent.setup>) {
    await user.type(await screen.findByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
  }

  it("asks for a code after the password and signs in with it", async () => {
    window.location.hash = "#/servers";
    const calls = hub({ "GET /api/agents?all=true": () => ({ data: [] }) });
    const user = userEvent.setup();
    render(<App />);

    await passwordStep(user);
    expect(await screen.findByRole("heading", { name: "Two-factor authentication" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Servers" })).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("Authentication code"), "000000");
    await user.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("the code is not valid");

    await user.clear(screen.getByLabelText("Authentication code"));
    await user.type(screen.getByLabelText("Authentication code"), "123456");
    await user.click(screen.getByRole("button", { name: "Verify" }));

    expect(await screen.findByRole("link", { name: "Servers" })).toBeInTheDocument();
    expect(calls.filter((c) => c.path === "/api/auth/login/two-factor").map((c) => c.body)).toEqual([
      { pending_token: "pending-1", code: "000000" },
      { pending_token: "pending-1", code: "123456" },
    ]);
  });

  it("goes back to the password", async () => {
    hub();
    const user = userEvent.setup();
    render(<App />);
    await passwordStep(user);
    await user.click(await screen.findByRole("button", { name: "Back" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("says when a recovery code was used", async () => {
    hub({}, { recovery_code_used: true, recovery_codes_left: 4 });
    window.location.hash = "#/servers";
    const user = userEvent.setup();
    render(<App />);
    await passwordStep(user);
    await user.type(await screen.findByLabelText("Authentication code"), "aaaaa-bbbbb");
    await user.click(screen.getByRole("button", { name: "Verify" }));
    expect(await screen.findByText(/signed in with a recovery code; 4 are left/)).toBeInTheDocument();
  });
});

describe("Required two-factor authentication", () => {
  it("makes the user set it up before anything else", async () => {
    let enrolled = false;
    mockHub({
      "GET /api/auth/me": () => ({
        data: { setup_required: false, user: { ...admin, two_factor: enrolled }, two_factor_setup_required: !enrolled },
      }),
      "GET /api/account/two-factor": () => ({ data: status({ enabled: enrolled, mandatory: true, policy: "admins", recovery_codes_left: enrolled ? 10 : 0 }) }),
      "POST /api/account/two-factor/setup": () => ({ data: setupInfo }),
      "POST /api/account/two-factor/enable": () => {
        enrolled = true;
        return { data: { recovery_codes: ["aaaaa-bbbbb"] } };
      },
      "GET /api/agents": () => ({ data: [] }),
      "GET /api/agents?all=true": () => ({ data: [] }),
      "GET /api/layout": () => ({ data: { layout: null, updated_at: null } }),
    });
    window.location.hash = "#/servers";
    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Set up two-factor authentication" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Servers" })).not.toBeInTheDocument();

    await user.type(await screen.findByLabelText("Your password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Set up two-factor authentication" }));
    await user.type(await screen.findByLabelText(/Enter the 6-digit code/), "123456");
    await user.click(screen.getByRole("button", { name: "Turn on two-factor authentication" }));
    await screen.findByRole("status", { name: "Recovery codes" });

    // Still on the setup page until the codes are saved.
    expect(screen.queryByRole("link", { name: "Servers" })).not.toBeInTheDocument();
    await user.click(screen.getByLabelText(/I saved these codes/));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(await screen.findByRole("link", { name: "Servers" })).toBeInTheDocument();
  });

  it("asks for the setup when the hub starts requiring it during a session", async () => {
    let required = false;
    mockHub({
      "GET /api/auth/me": () => ({ data: { setup_required: false, user: admin, two_factor_setup_required: required } }),
      "GET /api/account/two-factor": () => ({ data: status({ mandatory: true, policy: "all" }) }),
      "GET /api/agents": () => ({ data: [] }),
      "GET /api/agents?all=true": () => {
        required = true;
        return { status: 403, data: { error: "the hub requires two-factor authentication", code: "two_factor_setup_required" } };
      },
      "GET /api/layout": () => ({ data: { layout: null, updated_at: null } }),
    });
    window.location.hash = "#/servers";
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Set up two-factor authentication" })).toBeInTheDocument();
  });
});

describe("Two-factor administration", () => {
  const eve: User = { username: "eve", role: "viewer", permissions: VIEWER_PERMISSIONS, created_at: now, disabled: false, two_factor: true };

  function renderUsers() {
    return render(
      <SessionContext.Provider value={{ user: admin, signOut: () => Promise.resolve(), expired: vi.fn() }}>
        <UsersPage />
      </SessionContext.Provider>,
    );
  }

  it("lets an admin remove the second factor of a user", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let users = [admin, eve];
    const calls = mockHub({
      "GET /api/users": () => ({ data: users }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "GET /api/settings/security": () => ({ data: { two_factor_policy: "off", two_factor_available: true } }),
      "DELETE /api/users/eve/two-factor": () => {
        users = [admin, { ...eve, two_factor: false }];
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    renderUsers();

    await user.click(await screen.findByRole("button", { name: "Reset two-factor of eve" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Reset two-factor of eve" })).not.toBeInTheDocument());
    // Nothing to reset for users without it, nor for oneself.
    expect(screen.queryByRole("button", { name: "Reset two-factor of admin" })).not.toBeInTheDocument();
  });

  it("lets an admin choose who must use two-factor", async () => {
    const calls = mockHub({
      "GET /api/users": () => ({ data: [admin] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "GET /api/settings/security": () => ({ data: { two_factor_policy: "off", two_factor_available: true } }),
      "PUT /api/settings/security": (body) => ({ data: { two_factor_policy: (body as { two_factor_policy: string }).two_factor_policy, two_factor_available: true } }),
    });
    const user = userEvent.setup();
    renderUsers();

    const select = await screen.findByLabelText("Who must use two-factor authentication");
    await user.selectOptions(select, "admins");
    await waitFor(() => expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ two_factor_policy: "admins" }));
    expect(select).toHaveValue("admins");
  });

  it("cannot require two-factor when the hub has no secret key", async () => {
    mockHub({
      "GET /api/users": () => ({ data: [admin] }),
      "GET /api/audit?limit=100": () => ({ data: [] }),
      "GET /api/settings/security": () => ({ data: { two_factor_policy: "off", two_factor_available: false } }),
    });
    renderUsers();
    expect(await screen.findByLabelText("Who must use two-factor authentication")).toBeDisabled();
  });
});
