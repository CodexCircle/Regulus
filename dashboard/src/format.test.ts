import { describe, expect, it } from "vitest";
import { formatAgo, formatBytes, formatDuration, formatPercent, formatRate } from "./format";

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

describe("formatBytes", () => {
  it("uses binary units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(16_000_000 * 1024)).toBe("15.3 GiB");
    expect(formatBytes(200 * 1024 ** 3)).toBe("200 GiB");
    expect(formatBytes(-5)).toBe("0 B");
  });

  it("formats rates", () => {
    expect(formatRate(2048)).toBe("2.0 KiB/s");
  });
});

describe("formatPercent", () => {
  it("keeps a decimal below 10", () => {
    expect(formatPercent(0)).toBe("0.0%");
    expect(formatPercent(7.26)).toBe("7.3%");
    expect(formatPercent(42.4)).toBe("42%");
    expect(formatPercent(100)).toBe("100%");
  });
});
