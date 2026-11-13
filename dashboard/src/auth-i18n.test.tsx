import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthGate } from "./AuthGate";
import { ResetPage } from "./ResetPage";

function mockMe(data: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data) })),
  );
}

describe("auth pages in Italian", () => {
  beforeEach(() => {
    window.localStorage.setItem("regulus.language", "it");
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    window.location.hash = "";
  });

  it("shows the sign-in page", async () => {
    mockMe({ setup_required: false });
    render(<AuthGate>{() => <p>inside</p>}</AuthGate>);
    expect(await screen.findByRole("heading", { name: "Accedi" })).toBeInTheDocument();
    expect(screen.getByLabelText("Nome utente")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Accedi" })).toBeInTheDocument();
  });

  it("shows the first-run setup page", async () => {
    mockMe({ setup_required: true });
    render(<AuthGate>{() => <p>inside</p>}</AuthGate>);
    expect(await screen.findByRole("heading", { name: "Crea l'account amministratore" })).toBeInTheDocument();
    expect(screen.getByLabelText("Ripeti la password")).toBeInTheDocument();
  });

  it("shows the password reset page", () => {
    window.location.hash = "#/reset/tok";
    render(<ResetPage />);
    expect(screen.getByRole("heading", { name: "Scegli una nuova password" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cambia password" })).toBeInTheDocument();
  });
});
