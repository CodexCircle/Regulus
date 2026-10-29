# Regulus

**CodexCircle Server Manager**, also known as **Regulus**, is a platform to manage one or more Linux servers from a single web dashboard.

> **Project status:** early development. Hub, built-in and remote agents, dashboard, authentication and updates are implemented; plugins are not yet. No release has been published, and the release, the packages and the update path have not yet been tested on a real system (#81). See the [roadmap](#roadmap) and the [design decisions](docs/decisions.md).

## How it works

Regulus is built around **plugins**. The base installation provides a simple dashboard, and every plugin adds features and tools.

- **Dashboard**: fully web-based and customizable. Users choose which widgets to show and how to arrange them.
- **Agent**: installed on every server to be managed. It connects the server to the user's server network.
- **Hub**: installed on one server per group. It manages the agents connected to it and serves the dashboard. The hub installer asks whether to include the agent as well, so the hub's own server can be managed like the others.
- **Plugins**: extend the platform with new server-side features and new dashboard widgets.

```
  Browser (React dashboard)
            │
            ▼
      ┌───────────┐
      │    Hub    │  one hub per server group
      └───────────┘
        ▲   ▲   ▲
        │   │   │    outbound connections from the agents (gRPC, mTLS)
   ┌────┘   │   └────┐
 Agent    Agent    Agent    one agent per managed server
```

Agents open the connection to the hub, so no inbound ports are needed on managed servers and everything works behind NAT or firewalls. For how the parts work together (trust, data, updates), see [docs/architecture.md](docs/architecture.md).

The words used here (hub, agent, built-in agent, role, channel…) are defined in the [glossary](docs/glossary.md).

## Tech stack

| Component | Technology |
|---|---|
| Agent and hub | [Go](https://go.dev/): a single `regulus` executable used as agent or hub depending on the role |
| Dashboard | [React](https://react.dev/) with TypeScript and Vite, embedded in the binary |
| Communication | gRPC with Protobuf as the contract, mTLS between agent and hub |
| Plugins | Separate processes talking gRPC with the host, plus JavaScript modules for dashboard widgets |
| Data | SQLite today; PostgreSQL is planned (#122, #123, #124) |

## Supported platforms

Ubuntu Server LTS (22.04, 24.04 and 26.04, amd64 and arm64) is the reference platform; so far a release has been tested on 26.04 amd64. Debian and RHEL-like distributions follow.

## Roadmap

1. **Phase 1**: hub with built-in agent and dashboard.
2. **Phase 2**: remote agents on other servers.
3. **Phase 3**: separate agent and hub, protocol versioning, updates (manual, download-only or automatic).
4. **Phase 4**: plugins.

Third-party plugins come after the plugin system is stable. Work is tracked in the [issues](https://github.com/CodexCircle/Regulus/issues), grouped in milestones M1 to M6; [docs/decisions.md](docs/decisions.md#roadmap-phases-and-milestones) maps them to the phases.

## Quick install (Ubuntu Server)

```sh
curl -fsSLO https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh
sh install.sh
```

It downloads the latest release, verifies its checksum and signature, installs it, starts the service and prints where to open the dashboard and the one-time setup token. Until the first stable release is out, `latest` has nothing to serve: use the command at the top of the notes of a [pre-release](https://github.com/CodexCircle/Regulus/releases). Options, what the package sets up and how to add other servers are in [Installation](docs/installation.md). The dashboard speaks plain HTTP on port 8080: put a reverse proxy with TLS in front of it before exposing it beyond a trusted network.

## Documentation

The [documentation index](docs/README.md) says where to go for each task. The main pages:

- [Installation](docs/installation.md), [configuration](docs/configuration.md), [releases](docs/releases.md) and [updates](docs/updates.md)
- [Console and command line](docs/console-and-cli.md), [authentication](docs/authentication.md), [remote agents](docs/remote-agents.md), [HTTP API](docs/http-api.md) and [dashboard](docs/dashboard.md)
- [Architecture](docs/architecture.md) (with the code map), [glossary](docs/glossary.md), [design decisions](docs/decisions.md) and the [plugin design draft](docs/plugins.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), [development setup](docs/development.md) and the [design decisions](docs/decisions.md). Security problems: [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
