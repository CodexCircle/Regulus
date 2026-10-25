import { describe, expect, it } from "vitest";
import { resetTokenFromHash, routeFromHash } from "./route";

describe("routeFromHash", () => {
  it.each([
    ["", "/"],
    ["#", "/"],
    ["#/", "/"],
    ["#/servers", "/servers"],
    ["#/users", "/users"],
    ["#/account", "/account"],
    ["#/reset/abc_DEF-1", "/reset"],
    ["#/reset/", "/"],
    ["#/reset", "/"],
    ["#/nowhere", "/"],
  ])("%j is %s", (hash, want) => {
    expect(routeFromHash(hash)).toBe(want);
  });
});

describe("resetTokenFromHash", () => {
  it.each([
    ["#/reset/abc_DEF-1", "abc_DEF-1"],
    ["#/reset/a%20b", "a b"],
    ["#/reset/", ""],
    ["#/users", ""],
    ["", ""],
  ])("%j is %j", (hash, want) => {
    expect(resetTokenFromHash(hash)).toBe(want);
  });
});
