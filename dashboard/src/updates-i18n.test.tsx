import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RolloutStatus, UpdateStatus, User } from "./api";
import { LanguageProvider } from "./i18n";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS } from "./test-users";
import { UpdatesPage } from "./UpdatesPage";

const now = new Date().toISOString();
const admin: User = {
  username: "admin",
  role: "admin",
  permissions: ADMIN_PERMISSIONS,
  created_at: now,
  disabled: false,
  two_factor: false,
};

const status: UpdateStatus = {
  current_version: "0.1.0",
  release_build: true,
  checks_enabled: true,
  mode: "manual",
  channel: "stable",
  platform: "linux/amd64",
  checked_at: now,
  checking: false,
  latest: {
    version: "0.2.0",
    tag: "v0.2.0",
    prerelease: false,
    channel: "stable",
    url: "https://github.com/Stellatinus/Regulus/releases/tag/v0.2.0",
    notes: "Adds updates.",
    published_at: now,
  },
  update_available: true,
  staged: null,
  downloading: false,
  apply_enabled: false,
  apply_available: false,
  applying: false,
  last_apply: null,
};

const rollout: RolloutStatus = {
  target: "0.2.0",
  state: "idle",
  candidates: [
    { id: "agent-1", hostname: "web1", from: "0.1.0", status: "pending" },
    { id: "agent-2", hostname: "web2", from: "0.1.0", status: "pending" },
  ],
  agents: [],
  started_at: null,
  finished_at: null,
};

afterEach(() => vi.unstubAllGlobals());

describe("UpdatesPage in Italian", () => {
  it("shows the texts of the page in Italian", async () => {
    window.localStorage.setItem("regulus.language", "it");
    vi.stubGlobal(
      "fetch",
      vi.fn((path: string) =>
        Promise.resolve({
          ok: true,
          status: 200,
          json: () =>
            Promise.resolve(path === "/api/updates/rollout" ? rollout : status),
        }),
      ),
    );
    render(
      <LanguageProvider>
        <SessionContext.Provider
          value={{
            user: admin,
            signOut: () => Promise.resolve(),
            expired: vi.fn(),
          }}
        >
          <UpdatesPage />
        </SessionContext.Provider>
      </LanguageProvider>,
    );
    expect(
      await screen.findByRole("heading", { name: "Aggiornamenti" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("status", { name: "Aggiornamento disponibile" }),
    ).toHaveTextContent("Versione 0.2.0 è disponibile.");
    expect(
      screen.getByRole("button", { name: "Scarica 0.2.0" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Controlla ora" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("combobox", { name: "Canale" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("2 server eseguono una versione più vecchia."),
    ).toBeInTheDocument();
  });
});
