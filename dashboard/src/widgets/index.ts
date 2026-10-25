import { AgentsWidget } from "./AgentsWidget";
import { WidgetRegistry } from "./registry";

export { WidgetRegistry, type WidgetDefinition, type WidgetProps } from "./registry";

// registry is the registry of the running dashboard. Plugins add their widgets
// to it with registerWidget before the dashboard renders.
export const registry = new WidgetRegistry();

export const registerWidget = registry.register.bind(registry);

registerWidget({
  id: "agents",
  title: "Servers",
  description: "Connected servers with their status, last heartbeat and uptime.",
  defaultSize: { w: 12, h: 6 },
  minSize: { w: 4, h: 3 },
  inDefaultLayout: true,
  component: AgentsWidget,
});
