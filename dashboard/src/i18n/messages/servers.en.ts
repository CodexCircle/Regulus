// Texts of the Servers page, the server page, enrolling a server and the dashboard widgets (ServersPage, ServerPage, AgentList, Dashboard, widgets).
// Keys start with "servers.". Add the Italian text in servers.it.ts.
export const servers = {
  "servers.loading": "Loading…",

  // Labels shared by the server page, the list and the System widget.
  "servers.fact.id": "Id",
  "servers.fact.host": "Host",
  "servers.fact.server": "Server",
  "servers.fact.system": "System",
  "servers.fact.kernel": "Kernel",
  "servers.fact.cpus": "CPUs",
  "servers.fact.memory": "Memory",
  "servers.fact.status": "Status",
  "servers.fact.agentVersion": "Agent version",
  "servers.fact.lastContact": "Last contact",
  "servers.fact.loadAverage": "Load average",
  "servers.fact.uptime": "Uptime",

  // Server states. "online" is the list's word for connected; the System
  // widget shows the state as the hub names it.
  "servers.status.online": "online",
  "servers.status.connected": "connected",
  "servers.status.stale": "stale",
  "servers.status.offline": "offline",
  "servers.status.revoked": "revoked",
  "servers.status.incompatible": "incompatible",

  // The list of servers (AgentList).
  "servers.list.heading": "Agents",
  "servers.list.loadError": "Could not load agents: {message}",
  "servers.list.noServers": "No servers yet.",
  "servers.list.noneConnected": "No agents connected.",
  "servers.list.version": "Version",
  "servers.list.neverConnected": "never connected",
  "servers.list.noteTooOld": "too old for this hub: update it",
  "servers.list.noteBehind": "behind the hub",
  "servers.list.noteAhead": "ahead of the hub: update the hub",

  // The page of one server.
  "servers.server.back": "← All servers",
  "servers.server.loadError": "Could not load the server: {message}",
  "servers.server.notFound": "Server not found",
  "servers.server.notFoundBody": "There is no server with this id.",
  "servers.server.revokedNote":
    "This server was revoked and cannot connect any more. To use it again, remove its agent data directory and add it with a new token.",
  "servers.server.incompatibleNote":
    "The hub refuses this server's agent: {reason}. The agent keeps trying and connects once it is updated.",
  "servers.server.incompatibleFallback": "its version does not work with this hub",
  "servers.server.offlineNote": "This server is offline. Its metrics appear here again when its agent reconnects.",
  "servers.server.nameLabel": "Name",
  "servers.server.rename": "Rename",
  "servers.server.revoke": "Revoke this server",
  "servers.server.revokeConfirm": "Revoke {name}? It will be disconnected and can never connect again.",

  // Remote agents are off (ServersPage).
  "servers.remoteOff.title": "Remote agents are off",
  "servers.remoteOff.intro":
    "Only this hub's own server is managed. To add other servers, the hub must listen for their agents on a port of its own.",
  "servers.remoteOff.portLabel": "Port for remote agents",
  "servers.remoteOff.portHint": "Use a port from 1024 to 65535.",
  "servers.remoteOff.turnOn": "Turn on remote agents",
  "servers.remoteOff.turnOnNote":
    "The hub starts listening at once and keeps it after a restart. Open this port in the firewall towards the servers you want to manage.",
  "servers.remoteOff.askAdmin": "Someone who manages servers can turn them on here.",

  // Adding a server. "instructions" is used when the hub has no release
  // version; its wording (no comma after Regulus) is the one the page had
  // before the translation, kept as it was.
  "servers.add.title": "Add a server",
  "servers.add.nameLabel": "Name (optional)",
  "servers.add.createToken": "Create enrollment token",
  "servers.add.tokenLabel": "Enrollment token",
  "servers.add.instructions":
    "Run this on the server you want to add (Ubuntu or Debian, as a user that can use sudo). It installs Regulus enrolls the server and starts the agent. The token works once and expires at {expires}. It is not shown again.",
  "servers.add.instructionsVersion":
    "Run this on the server you want to add (Ubuntu or Debian, as a user that can use sudo). It installs Regulus {version}, the version of this hub, enrolls the server and starts the agent. The token works once and expires at {expires}. It is not shown again.",
  "servers.add.installCommand": "Install command",
  "servers.add.agentCommand": "Agent command",
  "servers.add.copyInstall": "Copy install command",
  "servers.add.copyAgent": "Copy agent command",
  "servers.add.copied": "Copied",
  "servers.add.replaceBefore": "Replace",
  "servers.add.replaceAfter":
    "if the server reaches the hub under another name. The fingerprint lets the server check that it is talking to this hub before it sends the token.",
  "servers.add.alreadyInstalled": "Regulus already installed on that server? Enroll and run the agent with:",

  // Widgets: titles and descriptions shown in the picker and the frames.
  "servers.widget.system.title": "System",
  "servers.widget.system.description": "What the server is: host, system, kernel, memory, load and uptime.",
  "servers.widget.cpu.title": "CPU",
  "servers.widget.cpu.description": "CPU usage now and over the last minutes, with the load average.",
  "servers.widget.memory.title": "Memory",
  "servers.widget.memory.description": "Memory in use now and over the last minutes, and swap.",
  "servers.widget.network.title": "Network",
  "servers.widget.network.description": "Received and sent traffic, in total and per interface.",
  "servers.widget.disk.title": "Disk usage",
  "servers.widget.disk.description": "How full each disk is.",
  "servers.widget.uptime.title": "Uptime",
  "servers.widget.uptime.description": "How long the server has been running.",
  "servers.widget.agents.title": "Servers",
  "servers.widget.agents.description": "Connected servers with their status, last heartbeat and uptime.",

  // Inside the widgets.
  "servers.widget.waiting": "Waiting for the first report…",
  "servers.widget.staleNote": "No news from this server since {ago}.",
  "servers.widget.connectionLost": "Connection to the hub lost, reconnecting…",
  "servers.widget.cpuLoad": "load {one} · {five} · {fifteen}",
  "servers.widget.cpuLoadCores": "load {one} · {five} · {fifteen} on {count} cores",
  "servers.widget.cpuChart": "CPU usage over the last minutes",
  "servers.widget.cpuSeries": "CPU",
  "servers.widget.memoryUsed": "{used} of {total} used",
  "servers.widget.memoryUsedSwap": "{used} of {total} used · swap {swapUsed} of {swapTotal}",
  "servers.widget.memoryChart": "Memory use over the last minutes",
  "servers.widget.noDisks": "No disks reported.",
  "servers.widget.diskUsage": "{used} of {total} ({percent})",
  "servers.widget.diskMeter": "{mount} used",
  "servers.widget.networkChart": "Network traffic over the last minutes",
  "servers.widget.received": "Received",
  "servers.widget.sent": "Sent",
  "servers.widget.interface": "Interface",
  "servers.widget.since": "since {date}",

  // Charts.
  "servers.chart.collecting": "Collecting data…",
  "servers.chart.lastMinutes": "last {minutes} min",

  // The dashboard.
  "servers.dashboard.label": "Dashboard",
  "servers.dashboard.loadError": "Could not load the dashboard: {message}",
  "servers.dashboard.done": "Done",
  "servers.dashboard.customize": "Customize",
  "servers.dashboard.addWidget": "Add widget",
  "servers.dashboard.confirmReset": "Replace your layout with the default?",
  "servers.dashboard.yesReset": "Yes, reset",
  "servers.dashboard.cancel": "Cancel",
  "servers.dashboard.reset": "Reset to default",
  "servers.dashboard.empty": "The dashboard is empty. Choose Customize, then Add widget.",
  "servers.dashboard.saving": "Saving…",
  "servers.dashboard.saved": "Saved",
  "servers.dashboard.saveError": "Could not save the layout",
  "servers.dashboard.available": "Available widgets",
  "servers.dashboard.add": "Add",
  "servers.dashboard.addNamed": "Add {title}",
  "servers.dashboard.removeNamed": "Remove {title}",
  "servers.dashboard.unavailable": "Unavailable widget",
  "servers.dashboard.notInstalledBefore": "The widget type",
  "servers.dashboard.notInstalledAfter": "is not installed. You can remove it.",
  "servers.dashboard.widgetFailed": "This widget failed: {message}",
} as const;
