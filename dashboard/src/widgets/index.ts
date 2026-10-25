import { AgentsWidget } from "./AgentsWidget";
import { CpuWidget, DiskWidget, MemoryWidget, NetworkWidget, SystemWidget, UptimeWidget } from "./BaseWidgets";
import { WidgetRegistry } from "./registry";

export { WidgetRegistry, type WidgetDefinition, type WidgetProps } from "./registry";

// registry is the registry of the running dashboard. Plugins add their widgets
// to it with registerWidget before the dashboard renders.
export const registry = new WidgetRegistry();

export const registerWidget = registry.register.bind(registry);

registerWidget({
  id: "system",
  title: "System",
  description: "What the server is: host, system, kernel, memory, load and uptime.",
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: SystemWidget,
});

registerWidget({
  id: "cpu",
  title: "CPU",
  description: "CPU usage now and over the last minutes, with the load average.",
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: CpuWidget,
});

registerWidget({
  id: "memory",
  title: "Memory",
  description: "Memory in use now and over the last minutes, and swap.",
  defaultSize: { w: 4, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: MemoryWidget,
});

registerWidget({
  id: "network",
  title: "Network",
  description: "Received and sent traffic, in total and per interface.",
  defaultSize: { w: 6, h: 6 },
  minSize: { w: 3, h: 3 },
  inDefaultLayout: true,
  component: NetworkWidget,
});

registerWidget({
  id: "disk",
  title: "Disk usage",
  description: "How full each disk is.",
  defaultSize: { w: 6, h: 6 },
  minSize: { w: 3, h: 2 },
  inDefaultLayout: true,
  component: DiskWidget,
});

registerWidget({
  id: "uptime",
  title: "Uptime",
  description: "How long the server has been running.",
  defaultSize: { w: 3, h: 2 },
  minSize: { w: 2, h: 2 },
  component: UptimeWidget,
});

registerWidget({
  id: "agents",
  title: "Servers",
  description: "Connected servers with their status, last heartbeat and uptime.",
  defaultSize: { w: 12, h: 4 },
  minSize: { w: 4, h: 3 },
  inDefaultLayout: true,
  component: AgentsWidget,
});
