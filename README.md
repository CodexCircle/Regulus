# Regulus

**English** · [Italiano](#italiano)

**CodexCircle Server Manager**, also known as **Regulus**, is a platform to manage one or more Linux servers from a single web dashboard.

> **Project status:** early development (Phase 1). The hub skeleton builds and serves a placeholder dashboard; the features below are the target design. See the [roadmap](#roadmap).

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

Agents open the connection to the hub, so no inbound ports are needed on managed servers and everything works behind NAT or firewalls.

## Tech stack

| Component | Technology |
|---|---|
| Agent and hub | [Go](https://go.dev/): a single `regulus` executable used as agent or hub depending on the role |
| Dashboard | [React](https://react.dev/) with TypeScript and Vite, embedded in the binary |
| Communication | gRPC with Protobuf as the contract, mTLS between agent and hub |
| Plugins | Separate processes talking gRPC with the host, plus JavaScript modules for dashboard widgets |
| Data | SQLite by default, PostgreSQL optional |

## Supported platforms

Ubuntu Server (22.04 and 24.04, amd64 and arm64) is the reference platform. Debian and RHEL-like distributions follow.

## Roadmap

1. **Phase 1**: hub with built-in agent and dashboard.
2. **Phase 2**: remote agents on other servers.
3. **Phase 3**: separate agent and hub, protocol versioning, updates (manual, download-only or automatic).
4. **Phase 4**: plugins.

Third-party plugins come after the plugin system is stable. Work is tracked in the [issues](https://github.com/CodexCircle/Regulus/issues).

## Development

Requirements: Go 1.25+, Node.js 22+.

```sh
make build            # builds the dashboard, then bin/regulus
./bin/regulus hub     # serves the dashboard on :8080
make test             # go vet, go test, dashboard typecheck
```

For dashboard work, run `npm run dev` in `dashboard/` (proxies `/api` to the hub on `:8080`).

## Repository layout

```
cmd/regulus/       single executable (role chosen with a subcommand)
internal/hub/      hub HTTP server
internal/protocol/ protocol version of the agent-hub contract
internal/inproc/   in-memory gRPC transport (used by the built-in agent)
internal/gen/      generated Go code for proto/ (do not edit)
proto/             Protobuf contract between agents and the hub
internal/webui/    embeds the compiled dashboard
dashboard/         React + TypeScript dashboard
```

Planned: `plugins/` (internal plugins), `packaging/` (systemd, `.deb`).

## License

Apache License 2.0. See [LICENSE](LICENSE).

---

# Italiano

**CodexCircle Server Manager**, detto anche **Regulus**, è una piattaforma per gestire uno o più server Linux da un'unica dashboard web.

> **Stato del progetto:** sviluppo iniziale (Fase 1). Lo scheletro dell'hub compila e serve una dashboard segnaposto; le funzioni descritte sono il design di destinazione. Vedi la [roadmap](#roadmap-1).

## Come funziona

Regulus lavora per **plug-in**. L'installazione base fornisce una dashboard semplice, e ogni plug-in aggiunge funzioni e strumenti.

- **Dashboard**: interamente web e personalizzabile. L'utente sceglie quali widget mostrare e come disporli.
- **Agent**: va installato su ogni server da gestire. Collega il server alla rete di server dell'utente.
- **Hub**: va installato su un server per gruppo. Gestisce gli agent a lui collegati e serve la dashboard. Durante l'installazione dell'hub viene chiesto se includere anche l'agent, così il server che ospita l'hub può essere gestito come gli altri.
- **Plug-in**: estendono la piattaforma con nuove funzioni lato server e nuovi widget lato dashboard.

Gli agent aprono la connessione verso l'hub, quindi non servono porte in ingresso sui server gestiti e il sistema funziona anche dietro NAT o firewall.

## Stack tecnologico

| Componente | Tecnologia |
|---|---|
| Agent e hub | [Go](https://go.dev/): un unico eseguibile `regulus`, usato come agent o come hub a seconda del ruolo |
| Dashboard | [React](https://react.dev/) con TypeScript e Vite, incorporata nel binario |
| Comunicazione | gRPC con Protobuf come contratto, mTLS tra agent e hub |
| Plug-in | Processi separati che parlano gRPC con l'host, più moduli JavaScript per i widget della dashboard |
| Dati | SQLite di default, PostgreSQL opzionale |

## Piattaforme supportate

Ubuntu Server (22.04 e 24.04, amd64 e arm64) è la piattaforma di riferimento. Seguiranno Debian e le distribuzioni RHEL-like.

## Roadmap

1. **Fase 1**: hub con agent integrato e dashboard.
2. **Fase 2**: agent remoti su altri server.
3. **Fase 3**: agent e hub separati, versionamento del protocollo, aggiornamenti (manuali, solo download o automatici).
4. **Fase 4**: plug-in.

I plug-in di terze parti arriveranno dopo la stabilizzazione del sistema di plug-in. Il lavoro è tracciato nelle [issue](https://github.com/CodexCircle/Regulus/issues).

## Sviluppo

Requisiti: Go 1.25+, Node.js 22+.

```sh
make build            # compila la dashboard, poi bin/regulus
./bin/regulus hub     # serve la dashboard su :8080
make test             # go vet, go test, typecheck della dashboard
```

Per lavorare sulla dashboard, esegui `npm run dev` in `dashboard/` (inoltra `/api` all'hub su `:8080`).

## Licenza

Apache License 2.0. Vedi [LICENSE](LICENSE).
