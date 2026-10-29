// Package hub implements the Regulus hub, the process that everything else talks
// to: it serves the dashboard and the HTTP API, accepts the connections of the
// agents, keeps the live state of the fleet and drives the updates.
//
// What it does, and where to start reading:
//
//   - hub.go: New builds a Hub from the configuration, Serve runs it. Serve
//     starts the HTTP server (dashboard, API, the /api/stream WebSocket) and,
//     when agent_listen is set, the gRPC server for remote agents.
//   - api.go, openapi.go, layout.go, updates_api.go: the HTTP API and its
//     OpenAPI description.
//   - agents.go, agent_net.go, local.go: the server side of the agent-hub
//     contract (proto/regulus/agent/v1): heartbeats, metrics, commands,
//     the in-process connection of the built-in agent, and refusal of agents
//     whose version is not accepted.
//   - fleet_api.go: enrollment tokens and the list of enrolled agents, on top of
//     package fleet.
//   - metrics.go, events.go, stream.go: samples kept in memory and the live
//     stream sent to the dashboard.
//   - rollout.go: the update of the agents in waves, driven by an administrator.
//
// The hub does not own the details of accounts and sessions (package auth),
// certificates (pki), the registry of agents (fleet), release download and
// verification (update) or the agent logic itself (agent). Redundancy and
// failover (#103) do not exist yet.
package hub
