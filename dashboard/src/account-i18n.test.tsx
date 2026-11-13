import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AccountPage } from "./AccountPage";
import type { SessionInfo, User } from "./api";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS } from "./test-users";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", permissions: ADMIN_PERMISSIONS, created_at: now, disabled: false, two_factor: false };
const sessions: SessionInfo[] = [
  { id: "aaa", remote_addr: "10.0.0.1", user_agent: "Firefox", created_at: now, last_used_at: now, expires_at: now, current: true },
  { id: "bbb", remote_addr: "10.0.0.9", user_agent: "Safari", created_at: now, last_used_at: now, expires_at: now, current: false },
];

function mockHub() {
  const answers: Record<string, unknown> = {
    "GET /api/account/sessions": sessions,
    "GET /api/account/two-factor": { enabled: false, mandatory: false, policy: "off", recovery_codes_left: 0 },
    "POST /api/account/two-factor/setup": { secret: "ABCD EFGH", uri: "otpauth://totp/x", qr: "data:image/png;base64,AAAA" },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const data = answers[`${init?.method ?? "GET"} ${path}`];
      const status = data === undefined ? 404 : 200;
      return Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(data ?? { error: "not found" }) });
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the account page in Italian", () => {
  it("shows the page, the sessions and the two-factor setup in Italian, data untouched", async () => {
    window.localStorage.setItem("regulus.language", "it");
    mockHub();
    render(
      <SessionContext.Provider value={{ user: admin, signOut: () => Promise.resolve(), expired: vi.fn() }}>
        <AccountPage />
      </SessionContext.Provider>,
    );
    const user = userEvent.setup();

    expect(screen.getByRole("heading", { name: "Account" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Cambia password" })).toBeInTheDocument();
    expect(screen.getByLabelText("Password attuale")).toBeInTheDocument();
    expect(await screen.findByText("adesso (questa sessione)")).toBeInTheDocument();
    expect(screen.getByText("Safari")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disconnetti 10.0.0.9" })).toBeInTheDocument();

    await user.type(await screen.findByLabelText("La tua password"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "Configura l'autenticazione a due fattori" }));
    expect(await screen.findByAltText(/Codice QR/)).toBeInTheDocument();
    expect(screen.getByLabelText("Chiave segreta")).toHaveTextContent("ABCD EFGH");
    expect(screen.getByRole("button", { name: "Attiva l'autenticazione a due fattori" })).toBeInTheDocument();
  });
});
