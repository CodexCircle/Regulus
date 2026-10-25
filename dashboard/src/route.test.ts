import { describe, expect, it } from "vitest";
import { routeFromHash } from "./route";

describe("routeFromHash", () => {
  it.each([
    ["", "/"],
    ["#", "/"],
    ["#/", "/"],
    ["#/servers", "/servers"],
    ["#/users", "/users"],
    ["#/account", "/account"],
    ["#/nowhere", "/"],
  ])("%j is %s", (hash, want) => {
    expect(routeFromHash(hash)).toBe(want);
  });
});
