import { describe, expect, it } from "vitest";
import { formatAgo, formatDuration } from "./format";

describe("formatDuration", () => {
  it.each([
    [0, "0s"],
    [8, "8s"],
    [310, "5m 10s"],
    [7500, "2h 5m"],
    [3 * 86400 + 4 * 3600, "3d 4h"],
    [-5, "0s"],
  ])("%d seconds is %s", (seconds, want) => {
    expect(formatDuration(seconds)).toBe(want);
  });
});

describe("formatAgo", () => {
  it("handles a missing timestamp", () => {
    expect(formatAgo(null)).toBe("never");
  });

  it("is relative to now", () => {
    const now = Date.parse("2026-10-25T12:00:10Z");
    expect(formatAgo("2026-10-25T12:00:00Z", now)).toBe("10s ago");
  });
});
