import type { WidgetRegistry } from "./widgets/registry";

export const GRID_COLS = 12;
export const LAYOUT_VERSION = 1;

// A widget placed on the dashboard.
export interface WidgetInstance {
  // Unique id of this placement; also the key in the grid.
  i: string;
  // Id of the widget type in the registry.
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  config?: Record<string, unknown>;
}

// DashboardLayout is what the hub stores for each user (PUT /api/layout). The
// hub does not interpret it.
export interface DashboardLayout {
  version: typeof LAYOUT_VERSION;
  widgets: WidgetInstance[];
}

const isInt = (v: unknown, min: number): v is number => typeof v === "number" && Number.isInteger(v) && v >= min;

// parseLayout validates a layout read from the hub. Widgets that are malformed
// or repeat an id are dropped; a value that is not a layout at all gives null.
export function parseLayout(raw: unknown): DashboardLayout | null {
  if (typeof raw !== "object" || raw === null) return null;
  const { version, widgets } = raw as { version?: unknown; widgets?: unknown };
  if (version !== LAYOUT_VERSION || !Array.isArray(widgets)) return null;

  const seen = new Set<string>();
  const valid: WidgetInstance[] = [];
  for (const w of widgets as unknown[]) {
    if (typeof w !== "object" || w === null) continue;
    const c = w as Record<string, unknown>;
    if (typeof c.i !== "string" || c.i === "" || seen.has(c.i) || typeof c.type !== "string") continue;
    if (!isInt(c.x, 0) || !isInt(c.y, 0) || !isInt(c.w, 1) || !isInt(c.h, 1) || c.x + c.w > GRID_COLS) continue;
    seen.add(c.i);
    const config = typeof c.config === "object" && c.config !== null && !Array.isArray(c.config) ? c.config : undefined;
    valid.push({ i: c.i, type: c.type, x: c.x, y: c.y, w: c.w, h: c.h, ...(config && { config: config as Record<string, unknown> }) });
  }
  return { version: LAYOUT_VERSION, widgets: valid };
}

// findSpot returns the first free place, scanning rows from the top and each
// row from the left, where a widget of the given size fits.
function findSpot(layout: DashboardLayout, w: number, h: number): { x: number; y: number } {
  const free = (x: number, y: number) =>
    layout.widgets.every((o) => x + w <= o.x || o.x + o.w <= x || y + h <= o.y || o.y + o.h <= y);
  for (let y = 0; ; y++) {
    for (let x = 0; x + w <= GRID_COLS; x++) {
      if (free(x, y)) return { x, y };
    }
  }
}

let counter = 0;
const defaultNewId = (type: string) => `${type}-${Date.now().toString(36)}${(counter++).toString(36)}`;

// addWidget places a new widget of the given type in the first free spot.
export function addWidget(
  layout: DashboardLayout,
  type: string,
  size: { w: number; h: number },
  newId: (type: string) => string = defaultNewId,
): DashboardLayout {
  const w = Math.min(size.w, GRID_COLS);
  const widget: WidgetInstance = { i: newId(type), type, ...findSpot(layout, w, size.h), w, h: size.h };
  return { ...layout, widgets: [...layout.widgets, widget] };
}

export function removeWidget(layout: DashboardLayout, i: string): DashboardLayout {
  return { ...layout, widgets: layout.widgets.filter((w) => w.i !== i) };
}

// setWidgetConfig replaces the settings saved with one widget.
export function setWidgetConfig(layout: DashboardLayout, i: string, config: Record<string, unknown>): DashboardLayout {
  return { ...layout, widgets: layout.widgets.map((w) => (w.i === i ? { ...w, config } : w)) };
}

// applyPositions copies positions and sizes from the grid (which may have
// compacted or moved items) into the layout. It returns the same layout object
// when nothing changed, so callers can skip a save.
export function applyPositions(
  layout: DashboardLayout,
  items: readonly { i: string; x: number; y: number; w: number; h: number }[],
): DashboardLayout {
  const byId = new Map(items.map((item) => [item.i, item]));
  let changed = false;
  const widgets = layout.widgets.map((w) => {
    const g = byId.get(w.i);
    if (!g || (g.x === w.x && g.y === w.y && g.w === w.w && g.h === w.h)) return w;
    changed = true;
    return { ...w, x: g.x, y: g.y, w: g.w, h: g.h };
  });
  return changed ? { ...layout, widgets } : layout;
}

// defaultLayout is what a user without a saved layout sees: the widgets that
// are marked for it, placed in registration order.
export function defaultLayout(registry: WidgetRegistry): DashboardLayout {
  let layout: DashboardLayout = { version: LAYOUT_VERSION, widgets: [] };
  for (const def of registry.list()) {
    if (def.inDefaultLayout) layout = addWidget(layout, def.id, def.defaultSize, (t) => t);
  }
  return layout;
}
