import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RoleInfo, User } from "./api";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS, CATALOGUE, VIEWER_PERMISSIONS } from "./test-users";
import { UsersPage } from "./UsersPage";

const now = new Date().toISOString();
const admin: User = { username: "admin", role: "admin", permissions: ADMIN_PERMISSIONS, created_at: now, disabled: false, two_factor: false };
const viewer: User = { username: "eve", role: "viewer", permissions: VIEWER_PERMISSIONS, created_at: now, disabled: false, two_factor: true };
const roles: RoleInfo[] = [
  { name: "admin", description: "", builtin: true, permissions: ADMIN_PERMISSIONS, users: 1 },
  { name: "viewer", description: "", builtin: true, permissions: VIEWER_PERMISSIONS, users: 1 },
];

afterEach(() => vi.unstubAllGlobals());

function stubHub() {
  const answers: Record<string, unknown> = {
    "/api/roles": { roles, permissions: CATALOGUE },
    "/api/users": [admin, viewer],
    "/api/settings/security": { two_factor_policy: "off", two_factor_available: true },
    "/api/audit?limit=100": [],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string) => {
      const data = answers[path];
      const ok = data !== undefined;
      return Promise.resolve({ ok, status: ok ? 200 : 404, json: () => Promise.resolve(data ?? { error: "not found" }) });
    }),
  );
}

describe("UsersPage in Italian", () => {
  it("shows the Italian texts of the page, the roles and the audit log", async () => {
    window.localStorage.setItem("regulus.language", "it");
    stubHub();
    render(
      <SessionContext.Provider value={{ user: admin, signOut: () => Promise.resolve(), expired: vi.fn() }}>
        <UsersPage />
      </SessionContext.Provider>,
    );
    expect(await screen.findByRole("heading", { name: "Utenti" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Disattiva eve" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Ruoli" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Sicurezza" })).toBeInTheDocument();
    expect(await screen.findByText("Ancora niente.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Aggiungi un ruolo" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Aggiungi utente" })).toBeInTheDocument();
  });

  it("refuses a user without permission in Italian", () => {
    window.localStorage.setItem("regulus.language", "it");
    render(
      <SessionContext.Provider value={{ user: viewer, signOut: () => Promise.resolve(), expired: vi.fn() }}>
        <UsersPage />
      </SessionContext.Provider>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Non hai il permesso di gestire gli utenti.");
  });
});
