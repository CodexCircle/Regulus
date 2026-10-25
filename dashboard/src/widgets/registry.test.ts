import { describe, expect, it } from "vitest";
import { registry } from "./index";
import { WidgetRegistry } from "./registry";

describe("WidgetRegistry", () => {
  const def = { id: "x", title: "X", description: "", defaultSize: { w: 1, h: 1 }, component: () => null };

  it("registers and finds widgets", () => {
    const r = new WidgetRegistry();
    r.register(def);
    expect(r.get("x")).toBe(def);
    expect(r.get("y")).toBeUndefined();
    expect(r.list()).toEqual([def]);
  });

  it("refuses a duplicate id", () => {
    const r = new WidgetRegistry();
    r.register(def);
    expect(() => r.register(def)).toThrow(/already registered/);
  });

  it("has the built-in servers widget", () => {
    expect(registry.get("agents")?.inDefaultLayout).toBe(true);
  });
});
