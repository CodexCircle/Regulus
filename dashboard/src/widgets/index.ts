import { AgentsWidget } from "./AgentsWidget";
import { CpuWidget, DiskWidget, MemoryWidget, NetworkWidget, SystemWidget, UptimeWidget } from "./BaseWidgets";
import { en } from "../i18n/en";
import { WidgetRegistry, type WidgetDefinition } from "./registry";

// texts gives a built-in widget its English title and description and the
// catalog keys the dashboard translates them with.
const texts = (name: "system" | "cpu" | "memory" | "network" | "disk" | "uptime" | "agents"): Pick<WidgetDefinition, "title" | "description" | "titleKey" | "descriptionKey"> => ({
  title: en[`servers.widget.${name}.title`],
  description: en[`servers.widget.${name}.description`],
  titleKey: `servers.widget.${name}.title`,
  descriptionKey: `servers.widget.${name}.description`,
});

export { WidgetRegistry, type WidgetDefinition, type WidgetProps } from "./registry";

// registry is the registry of the running dashboard. Plugins add their widgets
// to it with registerWidget before the dashboard renders.
export const registry = new WidgetRegistry();

export const registerWidget = registry.register.bind(registry);

registerWidget({
  id: "system",
  ...texts("system"),
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: SystemWidget,
});

registerWidget({
  id: "cpu",
  ...texts("cpu"),
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: CpuWidget,
});

registerWidget({
  id: "memory",
  ...texts("memory"),
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: MemoryWidget,
});

registerWidget({
  id: "network",
  ...texts("network"),
  defaultSize: { w: 6, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: NetworkWidget,
});

registerWidget({
  id: "disk",
  ...texts("disk"),
  defaultSize: { w: 6, h: 6 },
  minSize: { w: 3, h: 2 },
  inDefaultLayout: true,
  component: DiskWidget,
});

registerWidget({
  id: "uptime",
  ...texts("uptime"),
  defaultSize: { w: 3, h: 2 },
  minSize: { w: 2, h: 2 },
  component: UptimeWidget,
});

registerWidget({
  id: "agents",
  ...texts("agents"),
  defaultSize: { w: 12, h: 4 },
  minSize: { w: 4, h: 3 },
  inDefaultLayout: true,
  component: AgentsWidget,
});
