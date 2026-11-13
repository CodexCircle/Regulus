import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DatabaseBanner } from "./DatabaseBanner";

// database makes the fake hub answer /api/database with the given state, or fail.
let answer: { ok: boolean } | "fail" = { ok: true };
let asked = 0;

beforeEach(() => {
  vi.useFakeTimers();
  answer = { ok: true };
  asked = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string) => {
      expect(path).toBe("/api/database");
      asked++;
      if (answer === "fail") return Promise.reject(new TypeError("network down"));
      const body = answer;
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
    }),
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// tick moves the clock and lets the pending promises settle.
const tick = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

describe("DatabaseBanner", () => {
  it("shows nothing while the database answers", async () => {
    render(<DatabaseBanner />);
    await tick(0);
    expect(asked).toBe(1);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("warns when the database stops answering and says when it is back", async () => {
    render(<DatabaseBanner />);
    await tick(0);

    answer = { ok: false };
    await tick(10_000);
    expect(screen.getByRole("alert")).toHaveTextContent("The database is not answering");

    answer = { ok: true };
    await tick(10_000);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("The database is answering again.");

    // The good news goes away by itself.
    await tick(15_000);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("does not claim a problem when the hub cannot be reached", async () => {
    render(<DatabaseBanner />);
    await tick(0);
    answer = "fail";
    await tick(10_000);
    expect(screen.queryByRole("alert")).toBeNull();

    // And a failed request does not hide a problem already shown.
    answer = { ok: false };
    await tick(10_000);
    answer = "fail";
    await tick(10_000);
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it("speaks Italian to an Italian browser", async () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("it-IT");
    render(<DatabaseBanner />);
    answer = { ok: false };
    await tick(10_000);
    expect(screen.getByRole("alert")).toHaveTextContent("Il database non risponde");
  });
});
