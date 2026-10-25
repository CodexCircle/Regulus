import { describe, expect, it } from "vitest";
import { addWidget, applyPositions, defaultLayout, parseLayout, removeWidget, type DashboardLayout } from "./layout";
import { WidgetRegistry } from "./widgets/registry";

const empty: DashboardLayout = { version: 1, widgets: [] };

describe("parseLayout", () => {
  it("accepts a valid layout", () => {
    const raw = { version: 1, widgets: [{ i: "a", type: "agents", x: 0, y: 0, w: 6, h: 2, config: { k: 1 } }] };
    expect(parseLayout(raw)).toEqual(raw);
  });

  it.each([null, undefined, 3, "x", [], {}, { version: 2, widgets: [] }, { version: 1 }, { version: 1, widgets: "no" }])(
    "rejects %j",
    (raw) => {
      expect(parseLayout(raw)).toBeNull();
    },
  );

  it("drops malformed widgets and keeps the good ones", () => {
    const good = { i: "ok", type: "agents", x: 0, y: 0, w: 3, h: 2 };
    const layout = parseLayout({
      version: 1,
      widgets: [
        good,
        { ...good, i: "ok" }, // duplicate id
        { ...good, i: "" },
        { ...good, i: "b", w: 0 },
        { ...good, i: "c", x: 10, w: 4 }, // wider than the grid
        { ...good, i: "d", x: -1 },
        { ...good, i: "e", h: 1.5 },
        { ...good, i: "f", type: 7 },
        "nope",
        null,
      ],
    });
    expect(layout?.widgets).toEqual([good]);
  });

  it("ignores a config that is not an object", () => {
    const layout = parseLayout({ version: 1, widgets: [{ i: "a", type: "t", x: 0, y: 0, w: 1, h: 1, config: [1] }] });
    expect(layout?.widgets[0]).not.toHaveProperty("config");
  });
});

describe("editing a layout", () => {
  it("adds a widget below the others", () => {
    const withOne = addWidget(empty, "agents", { w: 12, h: 6 }, () => "a");
    const withTwo = addWidget(withOne, "cpu", { w: 4, h: 3 }, () => "b");
    expect(withTwo.widgets).toEqual([
      { i: "a", type: "agents", x: 0, y: 0, w: 12, h: 6 },
      { i: "b", type: "cpu", x: 0, y: 6, w: 4, h: 3 },
    ]);
    expect(empty.widgets).toHaveLength(0); // the original is untouched
  });

  it("limits the width to the grid and generates distinct ids", () => {
    const one = addWidget(empty, "t", { w: 40, h: 1 });
    const two = addWidget(one, "t", { w: 1, h: 1 });
    expect(one.widgets[0].w).toBe(12);
    expect(two.widgets[0].i).not.toBe(two.widgets[1].i);
  });

  it("removes a widget by id", () => {
    const layout = addWidget(addWidget(empty, "t", { w: 1, h: 1 }, () => "a"), "t", { w: 1, h: 1 }, () => "b");
    expect(removeWidget(layout, "a").widgets.map((w) => w.i)).toEqual(["b"]);
  });

  it("applies grid positions, and returns the same object when nothing moved", () => {
    const layout = addWidget(empty, "t", { w: 4, h: 2 }, () => "a");
    expect(applyPositions(layout, [{ i: "a", x: 0, y: 0, w: 4, h: 2 }])).toBe(layout);
    const moved = applyPositions(layout, [{ i: "a", x: 2, y: 3, w: 5, h: 4 }, { i: "ghost", x: 0, y: 0, w: 1, h: 1 }]);
    expect(moved.widgets).toEqual([{ i: "a", type: "t", x: 2, y: 3, w: 5, h: 4 }]);
  });
});

describe("defaultLayout", () => {
  it("stacks the widgets marked for the default layout", () => {
    const registry = new WidgetRegistry();
    const component = () => null;
    registry.register({ id: "one", title: "One", description: "", defaultSize: { w: 6, h: 2 }, inDefaultLayout: true, component });
    registry.register({ id: "optional", title: "Optional", description: "", defaultSize: { w: 6, h: 2 }, component });
    registry.register({ id: "two", title: "Two", description: "", defaultSize: { w: 6, h: 3 }, inDefaultLayout: true, component });
    expect(defaultLayout(registry).widgets).toEqual([
      { i: "one", type: "one", x: 0, y: 0, w: 6, h: 2 },
      { i: "two", type: "two", x: 0, y: 2, w: 6, h: 3 },
    ]);
  });
});
