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

## Installation (Ubuntu Server)

The hub installs on Ubuntu Server 22.04 and 24.04 (amd64 and arm64; other Debian-based systems with systemd should work too). One command downloads the `.deb` of the latest release, verifies its checksum and signature, installs it, starts the service and prints where to open the dashboard and the one-time setup token:

```sh
curl -fsSL https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh | sh
```

The installer asks whether to include the **built-in agent** (so the hub's own server is managed like the others). To answer in advance, or in a script, add options after `sh -s --`: `--builtin-agent`, `--no-builtin-agent`, `--yes` (no questions), `--beta` (latest pre-release), `--version 0.2.0`, `--require-signature` (fail unless the signature can be verified with [cosign](https://docs.sigstore.dev/cosign/)). Without cosign the installer verifies the checksum only and says so. If you prefer not to pipe a script into a shell, download the `.deb` from the [releases](https://github.com/CodexCircle/Regulus/releases), verify it as shown under [Releases](#releases) and run `sudo apt install ./regulus_*.deb`. While the repository is private, download the release files with `gh release download` instead.

What the package sets up:

| Path | Purpose |
|---|---|
| `/usr/bin/regulus` | the executable |
| `/usr/lib/systemd/system/regulus.service` | the service, enabled and started at install |
| `/etc/regulus/config.yaml` | configuration (kept on upgrade) |
| `/etc/regulus/regulus.env` | settings chosen by the installer, such as `REGULUS_BUILTIN_AGENT`; they override `config.yaml` |
| `/var/lib/regulus` | the database; kept when the package is removed or purged |

**Privilege model.** The hub runs as the system user `regulus`, which has no login shell, no home directory and no `sudo` rights, and the unit is hardened (no capabilities, read-only system, private `/tmp` and devices, system call filter). Monitoring needs no privileges, so this is enough for now. Actions that do need them (restarting services, package updates, Docker) will be added as explicit, opt-in grants rather than by running as root; see issue #46. Check the unit with `systemd-analyze security regulus`.

The dashboard speaks plain HTTP on port 8080. Before exposing the hub beyond a trusted network, put a reverse proxy with TLS in front of it and set `listen: 127.0.0.1:8080` in `/etc/regulus/config.yaml`.

## Development

Requirements: Go 1.26+, Node.js 22+.

```sh
make build            # builds the dashboard, then bin/regulus
./bin/regulus hub     # serves the dashboard on :8080
make test             # go vet, go test -race, dashboard typecheck and tests
make lint             # golangci-lint and ESLint
make smoke            # start bin/regulus and check it end to end (after make build)
scripts/test-install.sh   # tests of the install script (also part of make test)
```

For dashboard work, run `npm run dev` in `dashboard/` (proxies `/api` to the hub on `:8080`).

### Releases

Pushing a tag `vX.Y.Z` runs the release workflow (GoReleaser): it publishes a GitHub Release with `tar.gz` archives, `.deb` and `.rpm` packages (linux amd64 and arm64), SBOMs and `checksums.txt`, whose Sigstore signature is `checksums.txt.sigstore.json`. Tags with a suffix (`vX.Y.Z-beta.1`) are published as pre-releases (beta channel). To verify a download:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/CodexCircle/Regulus/.github/workflows/release.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

### Configuration

The hub reads, in increasing priority: defaults, a YAML file (`--config` or `REGULUS_CONFIG`), environment variables, flags.

| Setting (YAML) | Flag | Environment | Default |
|---|---|---|---|
| `listen` | `--listen` | `REGULUS_LISTEN` | `:8080` |
| `data_dir` | `--data-dir` | `REGULUS_DATA_DIR` | `data` |
| `builtin_agent` (`true`, `false`) | `--builtin-agent` | `REGULUS_BUILTIN_AGENT` | `true` |
| `metrics_interval` (for example `10s`, minimum `1s`) | `--metrics-interval` | `REGULUS_METRICS_INTERVAL` | `10s` |
| `log.level` (`debug`, `info`, `warn`, `error`) | `--log-level` | `REGULUS_LOG_LEVEL` | `info` |
| `log.format` (`text`, `json`) | `--log-format` | `REGULUS_LOG_FORMAT` | `text` |

With `builtin_agent` enabled (the default) the hub runs an agent for its own server, which connects through the same contract as any other agent (agent id `local`).

The hub stores its data in a SQLite database (`regulus.db`) inside `data_dir` and applies schema migrations automatically at startup.

### Authentication

The hub has local accounts with two roles, `admin` and `viewer`. On first start, with no accounts, the hub logs a one-time **setup token** (the line "no accounts yet", field `setup_token`); the dashboard asks for it together with the username and password of the first admin, so only someone who can read the hub log can claim an unconfigured hub. A new token is generated at each start until the admin exists.

- Passwords are hashed with argon2id (at least 10 characters). Login creates a session of 7 days stored in the database (only the SHA-256 of the token); the cookie is `HttpOnly`, `SameSite=Lax` and `Secure` when the request came over HTTPS (directly or via `X-Forwarded-Proto`). POST requests must be JSON from the same origin.
- After 10 failed logins from the same client address in 15 minutes, further attempts get `429` for the rest of the window. Behind a reverse proxy all clients share the proxy address, so put rate limiting in the proxy as well.
- Everything under `/api/` except `/api/auth/*` needs a login (`401` otherwise). The dashboard files and `/healthz` are public: they contain no data, and the page shows the login or setup form before anything else.
- Serve the hub over HTTPS (a reverse proxy is fine) outside a trusted network: sessions and passwords travel in the clear otherwise.

Endpoints: `GET /api/auth/me`, `POST /api/auth/setup`, `POST /api/auth/login`, `POST /api/auth/logout`.

**Users, sessions and audit.** Admins manage accounts in the dashboard (**Users**) or through the API: `GET`/`POST /api/users`, `PATCH`/`DELETE /api/users/{username}`. Viewers can read everything and change only their own dashboard layout; user management and the audit log are admin-only (`403` for viewers). The last enabled admin can never be demoted, disabled or deleted. Disabling or deleting an account ends its sessions at once, and a disabled account gets the same "invalid username or password" answer as a wrong password. Every user can change their own password (`POST /api/account/password`, which ends their other sessions and counts wrong current passwords toward the rate limit) and list or end their own sessions (`GET /api/account/sessions`, `DELETE /api/account/sessions/{id}`; each session records the address and browser it was created from). Account events (setup, logins and failed logins, user changes, password changes, ended sessions) go to an audit log in the database, newest 10,000 kept, readable by admins at `GET /api/audit?limit=` and in the **Users** page; it never stores passwords or tokens.

### HTTP API

`GET /api/agents` returns the connected agents as JSON (id, version, system info, `status` of `connected` or `stale`, last heartbeat, uptime). `GET /api/agents/{id}/metrics` returns the recent metrics of an agent (CPU, load, memory, swap, disks, network counters and rates, uptime), oldest first; `?since=<RFC 3339>` returns only newer points. The hub keeps the last hour of metrics in memory (up to 3600 reports per agent), so the history is lost when the hub restarts. `GET /api/agents/{id}` returns one agent. `GET /api/layout` and `PUT /api/layout` read and save the dashboard layout of the logged-in user (any JSON up to 64 KiB, stored per user). `GET /api/stream` upgrades to a **WebSocket** that pushes a `snapshot` of the agents and then `agent.connected`, `agent.disconnected`, `agent.heartbeat` and `agent.metrics` events as JSON; it needs the session cookie and refuses pages from other origins, and a client that reads too slowly is disconnected and should reconnect. All of this is described in [`internal/hub/openapi.yaml`](internal/hub/openapi.yaml), also served at `/api/openapi.yaml`; a test keeps the file in sync with the routes. The endpoints need a login and `GET`s are read-only; the dashboard shows the agents list on the **Servers** page.

### Dashboard

After the login the dashboard shows a header with the sections **Dashboard**, **Servers** and (for admins) **Users**, and a link to your **Account** page (change password, your sessions). The **Dashboard** page is a grid of widgets: **Customize** unlocks dragging and resizing, **Add widget** picks from the registered widgets, and **Reset to default** restores the initial layout. The layout is saved per user through `/api/layout` (with debouncing, and flushed when leaving the page). Widgets are registered in `dashboard/src/widgets/` with `registerWidget` (id, title, default size, component); a widget that crashes only breaks its own frame, and a layout that refers to an unknown widget (for example from a removed plugin) shows a placeholder that can be removed.

The base installation ships these widgets, fed live by the hub (the dashboard follows `/api/stream` and reconnects by itself if the connection drops): **System** (host, OS, kernel, memory, load, uptime), **CPU** and **Memory** (current value plus a chart of the last 10 minutes), **Network** (received and sent rate, in total and per interface), **Disk usage** (one bar per mounted filesystem), **Uptime**, and **Servers**. The first five (without Uptime) are in the default layout. When several agents are connected, each widget has a selector and remembers the choice; by default it shows the hub's built-in agent.

## Repository layout

```
cmd/regulus/       single executable (role chosen with a subcommand)
internal/config/   configuration (defaults, YAML file, environment, flags)
internal/logging/  structured logging
internal/storage/  SQLite database and migrations
internal/auth/     local accounts, sessions and the HTTP login endpoints
internal/hub/      hub: HTTP server, agents API and the agent service (registry of connected agents)
internal/agent/    agent runtime (client side of the contract) and Linux metrics collector, also used as the built-in agent
internal/protocol/ protocol version of the agent-hub contract
internal/inproc/   in-memory gRPC transport (used by the built-in agent)
internal/gen/      generated Go code for proto/ (do not edit)
internal/webui/    embeds the compiled dashboard
dashboard/         React + TypeScript dashboard
packaging/         systemd unit, default config, maintainer scripts and install.sh
proto/             Protobuf contract between agents and the hub
```

Planned: `plugins/` (internal plugins).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security problems: [SECURITY.md](SECURITY.md).

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

## Installazione (Ubuntu Server)

L'hub si installa su Ubuntu Server 22.04 e 24.04 (amd64 e arm64; dovrebbe funzionare anche su altri sistemi basati su Debian con systemd). Un solo comando scarica il `.deb` dell'ultima release, ne verifica checksum e firma, lo installa, avvia il servizio e mostra dove aprire la dashboard e il token di setup monouso:

```sh
curl -fsSL https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh | sh
```

L'installer chiede se includere l'**agent integrato** (così il server dell'hub è gestito come gli altri). Per rispondere in anticipo, o in uno script, aggiungi opzioni dopo `sh -s --`: `--builtin-agent`, `--no-builtin-agent`, `--yes` (nessuna domanda), `--beta` (ultima pre-release), `--version 0.2.0`, `--require-signature` (fallisce se la firma non si può verificare con [cosign](https://docs.sigstore.dev/cosign/)). Senza cosign l'installer verifica solo il checksum e lo dice. Se preferisci non passare uno script a una shell, scarica il `.deb` dalle [release](https://github.com/CodexCircle/Regulus/releases), verificalo come descritto in [Rilasci](#rilasci) ed esegui `sudo apt install ./regulus_*.deb`. Finché il repository è privato, scarica i file della release con `gh release download`.

Cosa prepara il pacchetto:

| Percorso | Scopo |
|---|---|
| `/usr/bin/regulus` | l'eseguibile |
| `/usr/lib/systemd/system/regulus.service` | il servizio, abilitato e avviato all'installazione |
| `/etc/regulus/config.yaml` | configurazione (conservata negli aggiornamenti) |
| `/etc/regulus/regulus.env` | impostazioni scelte dall'installer, come `REGULUS_BUILTIN_AGENT`; hanno la precedenza su `config.yaml` |
| `/var/lib/regulus` | il database; resta se il pacchetto viene rimosso o purgato |

**Modello dei privilegi.** L'hub gira con l'utente di sistema `regulus`, senza shell di login, senza home e senza diritti `sudo`, e l'unità è rinforzata (nessuna capability, sistema in sola lettura, `/tmp` e dispositivi privati, filtro delle system call). Il monitoraggio non richiede privilegi, quindi per ora basta. Le azioni che li richiedono (riavvio servizi, aggiornamenti dei pacchetti, Docker) arriveranno come permessi espliciti e facoltativi, non eseguendo come root; vedi l'issue #46. Controlla l'unità con `systemd-analyze security regulus`.

La dashboard usa HTTP in chiaro sulla porta 8080. Prima di esporre l'hub oltre una rete fidata, metti davanti un reverse proxy con TLS e imposta `listen: 127.0.0.1:8080` in `/etc/regulus/config.yaml`.

## Sviluppo

Requisiti: Go 1.26+, Node.js 22+.

```sh
make build            # compila la dashboard, poi bin/regulus
./bin/regulus hub     # serve la dashboard su :8080
make test             # go vet, go test -race, typecheck e test della dashboard
make lint             # golangci-lint ed ESLint
make smoke            # avvia bin/regulus e lo verifica end to end (dopo make build)
scripts/test-install.sh   # test dello script di installazione (incluso in make test)
```

Per lavorare sulla dashboard, esegui `npm run dev` in `dashboard/` (inoltra `/api` all'hub su `:8080`).

### Rilasci

Il push di un tag `vX.Y.Z` avvia il workflow di release (GoReleaser): pubblica una GitHub Release con archivi `tar.gz`, pacchetti `.deb` e `.rpm` (linux amd64 e arm64), SBOM e `checksums.txt`, la cui firma Sigstore è `checksums.txt.sigstore.json`. I tag con suffisso (`vX.Y.Z-beta.1`) sono pubblicati come pre-release (canale beta). Per verificare un download usa i comandi riportati nella sezione inglese.

### Configurazione

L'hub legge, in ordine di priorità crescente: valori predefiniti, file YAML (`--config` o `REGULUS_CONFIG`), variabili d'ambiente, flag. Le impostazioni e i nomi sono nella tabella in inglese qui sopra.

Con `builtin_agent` attivo (predefinito) l'hub esegue un agent per il proprio server, che si collega con lo stesso contratto di qualsiasi altro agent (id `local`).

L'hub salva i dati in un database SQLite (`regulus.db`) dentro `data_dir` e applica automaticamente le migrazioni dello schema all'avvio.

### Autenticazione

L'hub ha account locali con due ruoli, `admin` e `viewer`. Al primo avvio, senza account, l'hub scrive nel log un **token di setup** monouso (la riga "no accounts yet", campo `setup_token`); la dashboard lo chiede insieme a nome utente e password del primo admin, così solo chi può leggere il log dell'hub può prendere possesso di un hub non configurato. A ogni avvio viene generato un nuovo token finché l'admin non esiste.

- Le password usano argon2id (almeno 10 caratteri). Il login crea una sessione di 7 giorni salvata nel database (solo lo SHA-256 del token); il cookie è `HttpOnly`, `SameSite=Lax` e `Secure` se la richiesta è arrivata in HTTPS (direttamente o con `X-Forwarded-Proto`). Le richieste POST devono essere JSON dalla stessa origine.
- Dopo 10 login falliti dallo stesso indirizzo in 15 minuti, i tentativi successivi ricevono `429` fino a fine finestra. Dietro un reverse proxy tutti i client condividono l'indirizzo del proxy, quindi limita le richieste anche nel proxy.
- Tutto sotto `/api/` tranne `/api/auth/*` richiede il login (altrimenti `401`). I file della dashboard e `/healthz` sono pubblici: non contengono dati e la pagina mostra login o setup prima di tutto.
- Fuori da una rete fidata servi l'hub in HTTPS (va bene un reverse proxy): altrimenti sessioni e password viaggiano in chiaro.

Endpoint: `GET /api/auth/me`, `POST /api/auth/setup`, `POST /api/auth/login`, `POST /api/auth/logout`.

**Utenti, sessioni e audit.** Gli admin gestiscono gli account dalla dashboard (**Users**) o con l'API: `GET`/`POST /api/users`, `PATCH`/`DELETE /api/users/{username}`. I viewer leggono tutto e modificano solo il proprio layout della dashboard; la gestione utenti e il log di audit sono riservati agli admin (`403` per i viewer). L'ultimo admin attivo non può mai essere declassato, disabilitato o eliminato. Disabilitare o eliminare un account ne chiude subito le sessioni, e un account disabilitato riceve la stessa risposta "invalid username or password" di una password errata. Ogni utente può cambiare la propria password (`POST /api/account/password`, che chiude le altre sue sessioni e conta le password attuali errate nel limite di tentativi) ed elencare o chiudere le proprie sessioni (`GET /api/account/sessions`, `DELETE /api/account/sessions/{id}`; ogni sessione registra indirizzo e browser di creazione). Gli eventi degli account (setup, login e login falliti, modifiche agli utenti, cambi password, sessioni chiuse) vanno in un log di audit nel database, con gli ultimi 10.000 conservati, leggibile dagli admin con `GET /api/audit?limit=` e nella pagina **Users**; non salva mai password o token.

### API HTTP

`GET /api/agents` restituisce in JSON gli agent connessi (id, versione, info di sistema, `status` `connected` o `stale`, ultimo heartbeat, uptime). `GET /api/agents/{id}/metrics` restituisce le metriche recenti di un agent (CPU, load, memoria, swap, dischi, contatori e velocità di rete, uptime), dalla più vecchia; `?since=<RFC 3339>` restituisce solo i punti più recenti. L'hub tiene in memoria l'ultima ora di metriche (fino a 3600 report per agent), quindi lo storico si perde al riavvio dell'hub. `GET /api/agents/{id}` restituisce un solo agent. `GET /api/layout` e `PUT /api/layout` leggono e salvano il layout della dashboard dell'utente autenticato (qualsiasi JSON fino a 64 KiB, per utente). `GET /api/stream` passa a **WebSocket** e invia in JSON uno `snapshot` degli agent e poi gli eventi `agent.connected`, `agent.disconnected`, `agent.heartbeat` e `agent.metrics`; richiede il cookie di sessione, rifiuta le pagine di altre origini e disconnette il client che legge troppo lentamente (deve riconnettersi). Tutto è descritto in [`internal/hub/openapi.yaml`](internal/hub/openapi.yaml), servito anche su `/api/openapi.yaml`; un test mantiene il file allineato alle route. Gli endpoint richiedono il login e le `GET` sono in sola lettura; la dashboard mostra l'elenco degli agent nella pagina **Servers**.

### Dashboard

Dopo il login la dashboard mostra un'intestazione con le sezioni **Dashboard**, **Servers** e (per gli admin) **Users**, e un link alla pagina **Account** (cambio password, le tue sessioni). La pagina **Dashboard** è una griglia di widget: **Customize** abilita trascinamento e ridimensionamento, **Add widget** sceglie tra i widget registrati e **Reset to default** ripristina il layout iniziale. Il layout è salvato per utente tramite `/api/layout` (con debounce, e salvato anche quando si lascia la pagina). I widget si registrano in `dashboard/src/widgets/` con `registerWidget` (id, titolo, dimensione predefinita, componente); un widget che va in errore rompe solo il proprio riquadro, e un layout che cita un widget sconosciuto (ad esempio di un plugin rimosso) mostra un segnaposto che si può rimuovere.

L'installazione base include questi widget, alimentati in tempo reale dall'hub (la dashboard segue `/api/stream` e si riconnette da sola se la connessione cade): **System** (host, sistema operativo, kernel, memoria, load, uptime), **CPU** e **Memory** (valore attuale e grafico degli ultimi 10 minuti), **Network** (velocità in ricezione e invio, totale e per interfaccia), **Disk usage** (una barra per filesystem montato), **Uptime** e **Servers**. Tutti tranne Uptime sono nel layout predefinito. Con più agent collegati ogni widget ha un selettore e ricorda la scelta; di default mostra l'agent integrato dell'hub.

## Contribuire

Vedi [CONTRIBUTING.md](CONTRIBUTING.md#italiano). Problemi di sicurezza: [SECURITY.md](SECURITY.md#italiano).

## Licenza

Apache License 2.0. Vedi [LICENSE](LICENSE).
