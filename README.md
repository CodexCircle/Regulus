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
| `/usr/lib/systemd/system/regulus-agent.service` | the agent service, used only on servers installed with `--role agent` |
| `/etc/regulus/regulus.env` | settings chosen by the installer, such as `REGULUS_BUILTIN_AGENT`; they override `config.yaml` |
| `/etc/regulus/agent.yaml`, `/etc/regulus/role` | agent role only: the hub address, and a marker that keeps the package from starting a hub |
| `/var/lib/regulus` | the database; kept when the package is removed or purged |
| `/var/lib/regulus-agent` | agent role only: the agent's key and certificates; kept on removal |

**Privilege model.** The hub runs as the system user `regulus`, which has no login shell, no home directory and no `sudo` rights, and the unit is hardened (no capabilities, read-only system, private `/tmp` and devices, system call filter). Monitoring needs no privileges, so this is enough for now. Actions that do need them (restarting services, package updates, Docker) will be added as explicit, opt-in grants rather than by running as root; see issue #46. Check the unit with `systemd-analyze security regulus`.

**Installing an agent on another server.** The same installer sets up the agent role. In the dashboard open **Servers → Add a server**, which shows the command with a one-time token and the hub's CA fingerprint, and run it on the new server (Ubuntu or Debian, as a user that can use `sudo`):

```sh
curl -fsSL https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh | sh -s -- --role agent --hub HUB_HOST:8443 --token TOKEN --ca-fingerprint sha256:...
```

It verifies and installs the same package, enrolls the server as the `regulus` user (the token is sent only to a hub whose CA has that fingerprint), then enables and starts `regulus-agent.service`; the server shows as online within seconds. The hub must have the agent listener on (see [Remote agents](#remote-agents)). Keep the token off the command line with `REGULUS_AGENT_TOKEN=... sh -s -- --role agent ...`. Install the same version as the hub: add `--version`. If the hub revokes the agent, the service stops and is not restarted (exit status 3).

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

Endpoints: `GET /api/auth/me`, `POST /api/auth/setup`, `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/reset`.

**Users, sessions and audit.** Admins manage accounts in the dashboard (**Users**) or through the API: `GET`/`POST /api/users`, `PATCH`/`DELETE /api/users/{username}`. Viewers can read everything and change only their own dashboard layout; user management and the audit log are admin-only (`403` for viewers). The last enabled admin can never be demoted, disabled or deleted. Disabling or deleting an account ends its sessions at once, and a disabled account gets the same "invalid username or password" answer as a wrong password. Every user can change their own password (`POST /api/account/password`, which ends their other sessions and counts wrong current passwords toward the rate limit) and list or end their own sessions (`GET /api/account/sessions`, `DELETE /api/account/sessions/{id}`; each session records the address and browser it was created from). Account events (setup, logins and failed logins, user changes, password changes, ended sessions) go to an audit log in the database, newest 10,000 kept, readable by admins at `GET /api/audit?limit=` and in the **Users** page; it never stores passwords or tokens.

**Two-factor authentication.** Any user can add a second step to the sign-in from **Account → Two-factor authentication**: after confirming the password the dashboard shows a QR code (and the key, to type by hand) for any TOTP authenticator app (RFC 6238: Google Authenticator, Aegis, 1Password, Bitwarden…). Two-factor is on only after the user enters a valid code from the app, and then ten **recovery codes** are shown once (stored only as hashes; each works once instead of an app code; renewable, which voids the old ones). Afterwards a correct password does not create a session: the hub answers with a short-lived pending login (5 minutes, 5 tries, not a session) and the session starts only after a valid code. Codes are accepted within one 30-second step either side of the clock, and a code works once. Wrong codes count toward the failed-login limit above, and the password step does not reset it. Using a recovery code is audited, logged as a warning and shown to the user at sign-in with the number left. Turning it off, or making new recovery codes, needs the password and a code. TOTP secrets are stored encrypted (AES-256-GCM) with a key derived from `secret.key` in the data directory (created at the first start, readable by the owner only): a copy of the database alone does not reveal them, so back both up. In **Users → Security** an admin can require two-factor from administrators or from everyone; a user it applies to must set it up before anything else (the API answers `403` with `code: two_factor_setup_required` until then) and cannot turn it off. An admin can remove the second factor of a user who lost the app and the codes (**Reset two-factor**, audited; it ends their sessions). Endpoints: `POST /api/auth/login/two-factor`, `GET /api/account/two-factor`, `POST /api/account/two-factor/{setup,enable,disable,recovery-codes}`, `DELETE /api/users/{username}/two-factor`, `GET`/`PUT /api/settings/security`.

**Lost credentials.** The hub does not need email for recovery.

- *A user forgot the password:* an admin opens **Users**, picks **Reset password** and hands the link to the user (`POST /api/users/{username}/reset`). The link is `http://<hub>/#/reset/<token>`: the token is random, stored only as a hash, works once, expires after one hour and replaces any earlier link of that account; it sits in the URL fragment, so it never reaches server logs. The user opens it, chooses a new password (`POST /api/auth/reset`) and signs in; the admin never sees the password. Making the link ends all sessions of the account, and so does using it. Disabled accounts cannot be reset. Wrong tokens count toward the same limit as failed logins (10 per 15 minutes per address), and every token that is unknown, expired or used gets the same answer.
- *The admin is locked out:* on the machine that runs the hub, as the user that owns the data directory, run `sudo -u regulus regulus hub reset-admin --config /etc/regulus/config.yaml` (with the package install; otherwise pass the same `--config` or `--data-dir` as the hub, and `--user NAME` if there are several admins). It prints a one-time reset link like the one above for that admin, enables the account if it was disabled and ends its sessions; the hub can keep running. Whoever can write to the data directory owns the hub anyway, so that access is the proof of ownership. The password is chosen in the browser, so it never shows up in the terminal or the shell history. The command is recorded in the audit log as `console`.
- *The admin lost the authenticator app too:* `reset-admin` also removes the admin's two-factor authentication (console access is the proof of ownership), so the new password is enough to sign in; set it up again from **Account**. For other users, an admin uses **Reset two-factor**. Passkeys will be cleared by a reset when that feature exists.

### Remote agents

Besides the built-in agent, a server can run `regulus agent` and be managed from the hub. The agent always connects **out** to the hub (gRPC over TLS 1.3), so the managed server needs no open port.

1. **Turn on the listener.** Remote agents are off by default. Set `agent_listen: ":8443"` (or `--agent-listen`, `REGULUS_AGENT_LISTEN`) and restart the hub. Open that port in the firewall only towards the servers you manage.
2. **Make a token.** An admin opens **Servers → Add a server** in the dashboard, optionally names the server and gets the command to run, with a one-time token valid for one hour, the port and the **CA fingerprint** of the hub (the same thing `POST /api/enrollment-tokens` returns). The token is shown once.
3. **Enroll the agent** on the other server, once. On Ubuntu or Debian the easiest way is the installer command shown by the dashboard (see [Installation](#installation-ubuntu-server)); by hand, with the binary already there:
   ```sh
   regulus agent --hub HUB_HOST:8443 --token TOKEN --ca-fingerprint sha256:... --data-dir /var/lib/regulus-agent
   ```
   The agent first checks that the hub's certificate is signed by a CA with that fingerprint (so the token is never sent to an impostor), creates its own key, and the hub signs its certificate. The key stays on the agent. From then on the agent only needs `--hub` and the saved credentials; the same options can go in a YAML file (`--config`: `hub`, `data_dir`, `log`) or in `REGULUS_AGENT_*` variables.
4. **Revoke** an agent from its server page or with `DELETE /api/agents/{id}`: its connection is closed and it can never connect again; the process stops with an error. To use that server again, delete its data directory and enroll with a new token.

How it is protected: the hub creates its own certificate authority at the first start (`pki/` in the data directory, `ca.key` readable by the owner only). Tokens are random, stored only as a hash, single use, expire after an hour and are rate limited (10 wrong attempts per 15 minutes per address). Every connection needs the client certificate of an enrolled, not revoked agent, and the agent id comes from the certificate, not from what the agent says. Client certificates last five years; there is no renewal yet. Creating tokens, renaming and revoking agents are admin-only and go to the audit log.

### HTTP API

`GET /api/agents` returns the connected agents as JSON (id, version, system info, `status` of `connected` or `stale`, last heartbeat, uptime). `GET /api/agents/{id}/metrics` returns the recent metrics of an agent (CPU, load, memory, swap, disks, network counters and rates, uptime), oldest first; `?since=<RFC 3339>` returns only newer points. The hub keeps the last hour of metrics in memory (up to 3600 reports per agent), so the history is lost when the hub restarts. `GET /api/agents/{id}` returns one agent. `GET /api/agents?all=true` also lists the enrolled agents that are `offline` or `revoked`, with the time the hub last heard from them (`last_seen_at`); `PATCH /api/agents/{id}` (admin) gives an agent a display name. `GET /api/layout` and `PUT /api/layout` read and save the dashboard layout of the logged-in user (any JSON up to 64 KiB, stored per user). `GET /api/stream` upgrades to a **WebSocket** that pushes a `snapshot` of the agents and then `agent.connected`, `agent.disconnected`, `agent.heartbeat` and `agent.metrics` events as JSON; it needs the session cookie and refuses pages from other origins, and a client that reads too slowly is disconnected and should reconnect. All of this is described in [`internal/hub/openapi.yaml`](internal/hub/openapi.yaml), also served at `/api/openapi.yaml`; a test keeps the file in sync with the routes. The endpoints need a login and `GET`s are read-only; the dashboard shows the servers on the **Servers** page.

### Dashboard

After the login the dashboard shows a header with the sections **Dashboard**, **Servers** and (for admins) **Users**, and a link to your **Account** page (change password, your sessions). The **Dashboard** page is a grid of widgets: **Customize** unlocks dragging and resizing, **Add widget** picks from the registered widgets, and **Reset to default** restores the initial layout. The layout is saved per user through `/api/layout` (with debouncing, and flushed when leaving the page). Widgets are registered in `dashboard/src/widgets/` with `registerWidget` (id, title, default size, component); a widget that crashes only breaks its own frame, and a layout that refers to an unknown widget (for example from a removed plugin) shows a placeholder that can be removed.

The base installation ships these widgets, fed live by the hub (the dashboard follows `/api/stream` and reconnects by itself if the connection drops): **System** (host, OS, kernel, memory, load, uptime), **CPU** and **Memory** (current value plus a chart of the last 10 minutes), **Network** (received and sent rate, in total and per interface), **Disk usage** (one bar per mounted filesystem), **Uptime**, and **Servers**. The first five (without Uptime) are in the default layout. When several agents are connected, each widget has a selector and remembers the choice; by default it shows the hub's built-in agent.

The **Servers** page lists every enrolled server, online or not, with its system, agent version, status (online, stale, offline, revoked) and last contact. A row opens the page of that server: its facts and the System, CPU, Memory, Network and Disk usage widgets bound to it. Admins rename or revoke a server there and add new ones with **Add a server**. An offline server shows when it was last seen; a revoked one cannot connect again.

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
| `/usr/lib/systemd/system/regulus-agent.service` | il servizio dell'agent, usato solo sui server installati con `--role agent` |
| `/etc/regulus/regulus.env` | impostazioni scelte dall'installer, come `REGULUS_BUILTIN_AGENT`; hanno la precedenza su `config.yaml` |
| `/etc/regulus/agent.yaml`, `/etc/regulus/role` | solo ruolo agent: l'indirizzo dell'hub e un segnaposto che impedisce al pacchetto di avviare un hub |
| `/var/lib/regulus` | il database; resta se il pacchetto viene rimosso o purgato |
| `/var/lib/regulus-agent` | solo ruolo agent: chiave e certificati dell'agent; restano se si rimuove il pacchetto |

**Modello dei privilegi.** L'hub gira con l'utente di sistema `regulus`, senza shell di login, senza home e senza diritti `sudo`, e l'unità è rinforzata (nessuna capability, sistema in sola lettura, `/tmp` e dispositivi privati, filtro delle system call). Il monitoraggio non richiede privilegi, quindi per ora basta. Le azioni che li richiedono (riavvio servizi, aggiornamenti dei pacchetti, Docker) arriveranno come permessi espliciti e facoltativi, non eseguendo come root; vedi l'issue #46. Controlla l'unità con `systemd-analyze security regulus`.

**Installare un agent su un altro server.** Lo stesso installer prepara il ruolo agent. Nella dashboard apri **Servers → Add a server**, che mostra il comando con un token monouso e la fingerprint della CA dell'hub, ed eseguilo sul nuovo server (Ubuntu o Debian, con un utente che può usare `sudo`):

```sh
curl -fsSL https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh | sh -s -- --role agent --hub HOST_HUB:8443 --token TOKEN --ca-fingerprint sha256:...
```

Verifica e installa lo stesso pacchetto, registra il server come utente `regulus` (il token va solo a un hub la cui CA ha quella fingerprint), poi abilita e avvia `regulus-agent.service`; il server risulta online in pochi secondi. L'hub deve avere il listener degli agent attivo (vedi [Agent remoti](#agent-remoti)). Per tenere il token fuori dalla riga di comando usa `REGULUS_AGENT_TOKEN=... sh -s -- --role agent ...`. Installa la stessa versione dell'hub: aggiungi `--version`. Se l'hub revoca l'agent, il servizio si ferma e non viene riavviato (exit status 3).

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

Endpoint: `GET /api/auth/me`, `POST /api/auth/setup`, `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/reset`.

**Utenti, sessioni e audit.** Gli admin gestiscono gli account dalla dashboard (**Users**) o con l'API: `GET`/`POST /api/users`, `PATCH`/`DELETE /api/users/{username}`. I viewer leggono tutto e modificano solo il proprio layout della dashboard; la gestione utenti e il log di audit sono riservati agli admin (`403` per i viewer). L'ultimo admin attivo non può mai essere declassato, disabilitato o eliminato. Disabilitare o eliminare un account ne chiude subito le sessioni, e un account disabilitato riceve la stessa risposta "invalid username or password" di una password errata. Ogni utente può cambiare la propria password (`POST /api/account/password`, che chiude le altre sue sessioni e conta le password attuali errate nel limite di tentativi) ed elencare o chiudere le proprie sessioni (`GET /api/account/sessions`, `DELETE /api/account/sessions/{id}`; ogni sessione registra indirizzo e browser di creazione). Gli eventi degli account (setup, login e login falliti, modifiche agli utenti, cambi password, sessioni chiuse) vanno in un log di audit nel database, con gli ultimi 10.000 conservati, leggibile dagli admin con `GET /api/audit?limit=` e nella pagina **Users**; non salva mai password o token.

**Autenticazione a due fattori.** Ogni utente può aggiungere un secondo passaggio al login da **Account → Two-factor authentication**: dopo aver confermato la password la dashboard mostra un QR code (e la chiave, da digitare a mano) per qualsiasi app TOTP (RFC 6238: Google Authenticator, Aegis, 1Password, Bitwarden…). Il secondo fattore si attiva solo dopo aver inserito un codice valido dell'app, e a quel punto vengono mostrati una sola volta dieci **codici di recupero** (salvati solo come hash; ognuno vale una volta al posto di un codice dell'app; rinnovabili, il che invalida i vecchi). Da allora una password corretta non crea una sessione: l'hub risponde con un login in sospeso di breve durata (5 minuti, 5 tentativi, non è una sessione) e la sessione nasce solo dopo un codice valido. I codici sono accettati entro un passo di 30 secondi prima o dopo l'orologio e ognuno vale una volta. I codici errati contano nel limite dei login falliti qui sopra, e il passaggio della password non lo azzera. Usare un codice di recupero viene registrato nell'audit e nel log come avviso, e all'accesso l'utente vede quanti ne restano. Disattivarlo o rifare i codici di recupero richiede password e un codice. I segreti TOTP sono salvati cifrati (AES-256-GCM) con una chiave derivata da `secret.key` nella directory dati (creato al primo avvio, leggibile solo dal proprietario): una copia del solo database non li rivela, quindi fai il backup di entrambi. In **Users → Security** un admin può rendere obbligatoria la 2FA per gli amministratori o per tutti; chi è interessato deve configurarla prima di tutto (l'API risponde `403` con `code: two_factor_setup_required` fino ad allora) e non può disattivarla. Un admin può rimuovere il secondo fattore a chi ha perso app e codici (**Reset two-factor**, registrato nell'audit; chiude le sue sessioni). Endpoint: `POST /api/auth/login/two-factor`, `GET /api/account/two-factor`, `POST /api/account/two-factor/{setup,enable,disable,recovery-codes}`, `DELETE /api/users/{username}/two-factor`, `GET`/`PUT /api/settings/security`.

**Credenziali perse.** Il recupero non richiede email.

- *Un utente ha dimenticato la password:* un admin apre **Users**, sceglie **Reset password** e consegna il link all'utente (`POST /api/users/{username}/reset`). Il link è `http://<hub>/#/reset/<token>`: il token è casuale, salvato solo come hash, vale una volta sola, scade dopo un'ora e sostituisce ogni link precedente dello stesso account; sta nel frammento dell'URL, quindi non arriva mai nei log del server. L'utente lo apre, sceglie la nuova password (`POST /api/auth/reset`) e accede; l'admin non vede mai la password. Creare il link chiude tutte le sessioni dell'account, e lo stesso fa usarlo. Gli account disabilitati non si possono azzerare. I token sbagliati contano nello stesso limite dei login falliti (10 ogni 15 minuti per indirizzo), e ogni token sconosciuto, scaduto o già usato riceve la stessa risposta.
- *L'admin è bloccato fuori:* sulla macchina che esegue l'hub, come l'utente proprietario della directory dati, esegui `sudo -u regulus regulus hub reset-admin --config /etc/regulus/config.yaml` (con l'installazione da pacchetto; altrimenti passa gli stessi `--config` o `--data-dir` dell'hub, e `--user NOME` se ci sono più admin). Stampa un link di reset monouso come sopra per quell'admin, riabilita l'account se era disabilitato e ne chiude le sessioni; l'hub può restare in esecuzione. Chi può scrivere nella directory dati è comunque il padrone dell'hub, quindi quell'accesso è la prova di proprietà. La password si sceglie nel browser, quindi non compare mai nel terminale né nella cronologia della shell. Il comando è registrato nell'audit come `console`.
- *L'admin ha perso anche l'app di autenticazione:* `reset-admin` rimuove anche la 2FA dell'admin (l'accesso alla console è la prova di proprietà), quindi basta la nuova password per entrare; riattivala da **Account**. Per gli altri utenti un admin usa **Reset two-factor**. Le passkey saranno cancellate da un reset quando questa funzione esisterà.

### Agent remoti

Oltre all'agent integrato, un server può eseguire `regulus agent` ed essere gestito dall'hub. L'agent si collega sempre **in uscita** verso l'hub (gRPC su TLS 1.3), quindi il server gestito non deve aprire nessuna porta.

1. **Attiva il listener.** Gli agent remoti sono spenti di default. Imposta `agent_listen: ":8443"` (oppure `--agent-listen`, `REGULUS_AGENT_LISTEN`) e riavvia l'hub. Apri quella porta nel firewall solo verso i server che gestisci.
2. **Crea un token.** Un admin apre **Servers → Add a server** nella dashboard, dà se vuole un nome al server e ottiene il comando da eseguire, con un token monouso valido un'ora, la porta e la **fingerprint della CA** dell'hub (le stesse cose che restituisce `POST /api/enrollment-tokens`). Il token viene mostrato una sola volta.
3. **Registra l'agent** sull'altro server, una volta sola. Su Ubuntu o Debian il modo più semplice è il comando dell'installer mostrato dalla dashboard (vedi [Installazione](#installazione-ubuntu-server)); a mano, con il binario già presente:
   ```sh
   regulus agent --hub HOST_HUB:8443 --token TOKEN --ca-fingerprint sha256:... --data-dir /var/lib/regulus-agent
   ```
   L'agent controlla prima che il certificato dell'hub sia firmato da una CA con quella fingerprint (così il token non va mai a un impostore), crea la propria chiave e l'hub firma il suo certificato. La chiave resta sull'agent. Da quel momento servono solo `--hub` e le credenziali salvate; le stesse opzioni possono stare in un file YAML (`--config`: `hub`, `data_dir`, `log`) o nelle variabili `REGULUS_AGENT_*`.
4. **Revoca** un agent dalla pagina del suo server o con `DELETE /api/agents/{id}`: la connessione si chiude e non potrà più collegarsi; il processo si ferma con un errore. Per usare di nuovo quel server, cancella la sua directory dati e registralo con un nuovo token.

Come è protetto: l'hub crea una propria autorità di certificazione al primo avvio (`pki/` nella directory dati, `ca.key` leggibile solo dal proprietario). I token sono casuali, salvati solo come hash, monouso, scadono dopo un'ora e hanno un limite di tentativi (10 errori ogni 15 minuti per indirizzo). Ogni connessione richiede il certificato client di un agent registrato e non revocato, e l'id dell'agent viene dal certificato, non da ciò che l'agent dichiara. I certificati client durano cinque anni; il rinnovo non c'è ancora. Creare token, rinominare e revocare agent è riservato agli admin e finisce nell'audit.

### API HTTP

`GET /api/agents` restituisce in JSON gli agent connessi (id, versione, info di sistema, `status` `connected` o `stale`, ultimo heartbeat, uptime). `GET /api/agents/{id}/metrics` restituisce le metriche recenti di un agent (CPU, load, memoria, swap, dischi, contatori e velocità di rete, uptime), dalla più vecchia; `?since=<RFC 3339>` restituisce solo i punti più recenti. L'hub tiene in memoria l'ultima ora di metriche (fino a 3600 report per agent), quindi lo storico si perde al riavvio dell'hub. `GET /api/agents/{id}` restituisce un solo agent. `GET /api/agents?all=true` elenca anche gli agent registrati `offline` o `revoked`, con l'ultimo momento in cui l'hub li ha sentiti (`last_seen_at`); `PATCH /api/agents/{id}` (admin) assegna un nome visualizzato. `GET /api/layout` e `PUT /api/layout` leggono e salvano il layout della dashboard dell'utente autenticato (qualsiasi JSON fino a 64 KiB, per utente). `GET /api/stream` passa a **WebSocket** e invia in JSON uno `snapshot` degli agent e poi gli eventi `agent.connected`, `agent.disconnected`, `agent.heartbeat` e `agent.metrics`; richiede il cookie di sessione, rifiuta le pagine di altre origini e disconnette il client che legge troppo lentamente (deve riconnettersi). Tutto è descritto in [`internal/hub/openapi.yaml`](internal/hub/openapi.yaml), servito anche su `/api/openapi.yaml`; un test mantiene il file allineato alle route. Gli endpoint richiedono il login e le `GET` sono in sola lettura; la dashboard mostra i server nella pagina **Servers**.

### Dashboard

Dopo il login la dashboard mostra un'intestazione con le sezioni **Dashboard**, **Servers** e (per gli admin) **Users**, e un link alla pagina **Account** (cambio password, le tue sessioni). La pagina **Dashboard** è una griglia di widget: **Customize** abilita trascinamento e ridimensionamento, **Add widget** sceglie tra i widget registrati e **Reset to default** ripristina il layout iniziale. Il layout è salvato per utente tramite `/api/layout` (con debounce, e salvato anche quando si lascia la pagina). I widget si registrano in `dashboard/src/widgets/` con `registerWidget` (id, titolo, dimensione predefinita, componente); un widget che va in errore rompe solo il proprio riquadro, e un layout che cita un widget sconosciuto (ad esempio di un plugin rimosso) mostra un segnaposto che si può rimuovere.

L'installazione base include questi widget, alimentati in tempo reale dall'hub (la dashboard segue `/api/stream` e si riconnette da sola se la connessione cade): **System** (host, sistema operativo, kernel, memoria, load, uptime), **CPU** e **Memory** (valore attuale e grafico degli ultimi 10 minuti), **Network** (velocità in ricezione e invio, totale e per interfaccia), **Disk usage** (una barra per filesystem montato), **Uptime** e **Servers**. Tutti tranne Uptime sono nel layout predefinito. Con più agent collegati ogni widget ha un selettore e ricorda la scelta; di default mostra l'agent integrato dell'hub.

La pagina **Servers** elenca tutti i server registrati, online o no, con sistema, versione dell'agent, stato (online, stale, offline, revoked) e ultimo contatto. Una riga apre la pagina di quel server: i suoi dati e i widget System, CPU, Memory, Network e Disk usage legati a lui. Gli admin lo rinominano o lo revocano da lì e ne aggiungono di nuovi con **Add a server**. Un server offline mostra quando è stato visto l'ultima volta; uno revocato non può più collegarsi.

## Contribuire

Vedi [CONTRIBUTING.md](CONTRIBUTING.md#italiano). Problemi di sicurezza: [SECURITY.md](SECURITY.md#italiano).

## Licenza

Apache License 2.0. Vedi [LICENSE](LICENSE).
