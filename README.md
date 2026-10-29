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

Ubuntu Server LTS (22.04, 24.04 and 26.04, amd64 and arm64) is the reference platform; so far a release has been tested on 26.04 amd64. Debian and RHEL-like distributions follow.

## Roadmap

1. **Phase 1**: hub with built-in agent and dashboard.
2. **Phase 2**: remote agents on other servers.
3. **Phase 3**: separate agent and hub, protocol versioning, updates (manual, download-only or automatic).
4. **Phase 4**: plugins.

Third-party plugins come after the plugin system is stable. Work is tracked in the [issues](https://github.com/CodexCircle/Regulus/issues), grouped in milestones M1 to M6; [docs/decisions.md](docs/decisions.md#roadmap-phases-and-milestones) maps them to the phases.

## Installation (Ubuntu Server)

The hub installs on Ubuntu Server 22.04, 24.04 and 26.04 (amd64 and arm64; other Debian-based systems with systemd should work too). One command downloads the `.deb` of the latest release, verifies its checksum and signature, installs it, starts the service and prints where to open the dashboard and the one-time setup token:

```sh
curl -fsSLO https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh
sh install.sh
```

The same in one line is `curl -fsSL …/install.sh | sh`, with options after `sh -s --`; if the `-s` gets lost, `sh` takes the first option for a file name ("cannot open --version"), so the two steps are safer. Until the first stable release is out, `latest` has nothing to serve: use the command at the top of the notes of a [pre-release](https://github.com/CodexCircle/Regulus/releases).

The installer asks whether to include the **built-in agent** (so the hub's own server is managed like the others). To answer in advance, or in a script, add options after `sh install.sh` (or `sh -s --` in the one-line form): `--builtin-agent`, `--no-builtin-agent`, `--yes` (no questions), `--channel beta` or `--channel alpha` (latest release of that channel; also `--beta`, `--alpha`), `--version 0.2.0`, `--require-signature` (fail unless the signature can be verified with [cosign](https://docs.sigstore.dev/cosign/)). Without cosign the installer verifies the checksum only and says so. If you prefer not to pipe a script into a shell, download the `.deb` from the [releases](https://github.com/CodexCircle/Regulus/releases), verify it as shown under [Releases](#releases) and run `sudo apt install ./regulus_*.deb`.

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

There are three channels: **stable** (`v0.3.0`), **beta** (`v0.3.0-beta.2`, or `-rc.1`) and **alpha** (`v0.3.0-alpha.1`). To publish, a maintainer runs **Publish release** in the Actions tab on `main`, picking the channel (and, when a new version starts, whether to raise the patch, minor or major number). It refuses a commit on which CI has not passed, works out the next version (`scripts/next-version.sh`: a version goes through its alphas, then its betas, then the release; pre-releases are numbered per channel), tags the commit and starts the release workflow on the tag. Pushing a `v*` tag by hand does the same.

The release workflow (GoReleaser) publishes a GitHub Release with `tar.gz` archives, `.deb` and `.rpm` packages (linux amd64 and arm64), SBOMs, `install.sh` and `checksums.txt`, whose Sigstore signature is `checksums.txt.sigstore.json`. Alphas and betas are marked as pre-releases, so GitHub's **Latest** (and `releases/latest/download/install.sh`) is always the newest stable release; each release's notes start with the install command for that exact version. To verify a download:

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
| `updates.check` (`true`, `false`) | - | `REGULUS_UPDATES_CHECK` | `true` |
| `updates.apply` (`true`, `false`) | - | `REGULUS_UPDATES_APPLY` | `false` (the installer sets it) |
| `console.tui` (`true`, `false`) | `--tui`, `--no-tui` (console commands only) | `REGULUS_NO_TUI=1` (the opposite) | `true` |

With `builtin_agent` enabled (the default) the hub runs an agent for its own server, which connects through the same contract as any other agent (agent id `local`).

The hub stores its data in a SQLite database (`regulus.db`) inside `data_dir` and applies schema migrations automatically at startup.

### Updates

The hub looks for new releases of Regulus, keeps the newest one downloaded and checked, and can install it on its own server from the dashboard, with a rollback. The servers you manage (the agents) are updated from the hub too.

- **Where from:** the [GitHub Releases](https://github.com/CodexCircle/Regulus/releases) of this repository, read without a token. The hub asks every six hours (a little later after a restart, and at most once a minute from the dashboard), with conditional requests so it stays well under GitHub's limit for anonymous use. Only the first request after a change of the list costs anything. A development build (`dev`, or one made after a tag with `git describe`) never asks by itself and is never told it is behind.
- **Channel:** `stable` offers releases only; `beta` also offers betas and release candidates (`v0.3.0-beta.1`, `v0.3.0-rc.1`); `alpha` offers every release, alphas included (`v0.3.0-alpha.1`, and any other pre-release). Each channel takes whichever offered release is newest, so it moves to the beta or the release when one comes out. A hub starts on the channel of the version it runs. Versions are compared as SemVer, so a hub never "updates" to something older.
- **Mode**, chosen in the dashboard (**Updates**, needs the `core.update` permission, which `admin` has and `operator` does not): `manual` only tells you; `download` also downloads the archive of this platform into `<data_dir>/updates/` and checks it, then waits for you to press **Install** (the default, so a new hub never installs anything without an administrator); `automatic` also installs it by itself, but only inside a maintenance window you set (two times such as 03:00 and 05:00 in the server's time zone, possibly across midnight), and never tries again a version that had to be rolled back. Every change of settings, every download and every installation is in the audit log.
- **What "checked" means:** first the signature of `checksums.txt`, then the archive. The release workflow signs `checksums.txt` with Sigstore (keyless, `checksums.txt.sigstore.json`); the hub verifies that bundle in-process, with no `cosign` needed: the certificate must name the release workflow of this repository **at the tag of the release**, issued by GitHub Actions, with a transparency log entry and a certificate timestamp. Only then is the archive downloaded and its SHA-256 compared with the list; a mismatch deletes it. A release with a missing or invalid signature is never kept. The Sigstore trusted root is fetched from Sigstore's TUF repository and cached in `<data_dir>/tuf` (a key rotation reaches the hub by itself); if that cannot be reached, a snapshot built into the binary is used.
- **Installing, and why no `sudo`:** the hub runs as the unprivileged user `regulus` and keeps it that way. The package ships `regulus-update.path`, which the installer enables (`install.sh --no-self-update` skips it) together with `updates.apply: true` (`REGULUS_UPDATES_APPLY`). When you press **Install** the hub leaves a request that names a version, nothing else; systemd starts `regulus-update.service` as root, which runs `regulus update-apply`. That helper **trusts nothing the hub's user could have written**: it copies the archive into a directory of its own, verifies the Sigstore signature of `checksums.txt` and the archive against it again, refuses a version that is not newer than the installed one, extracts the executable, checks that it reports the expected version, keeps the current binary in `/var/lib/regulus-update/regulus.previous`, replaces `/usr/bin/regulus` atomically and restarts the hub. It then watches the new hub (the service must be active and report the new version and stay up for 15 seconds, within 90); if not, the previous binary is put back and restarted, and the result is `rolled_back`. The hub restarts in the middle, so the dashboard is unreachable for a moment; the outcome (`applied`, `rolled_back`, `failed`) appears on the Updates page and in the audit log. Only the executable is replaced: a release that changes the systemd units or the configuration file needs the package (`install.sh`) once. Because dpkg does not know about the replaced file, `dpkg --verify regulus` reports it as changed, and a later `apt` reinstall of the package brings back the version in the package.
- **Updating the servers (agents):** the hub is updated first, then the servers, to the version the hub runs; an agent may be one release series behind (see *Versions*), so there is always a window in which both can run. The **Updates** page lists the connected servers that are behind and updates them a wave at a time (1, 3, 10 or all), stopping at the first failure, with a result per server and an entry in the audit log (`POST /api/updates/rollout`). In the `automatic` mode the hub does it by itself inside the maintenance window, and does not retry a rollout that failed. How it works: the hub makes sure its cache holds the signed release with the archive of each platform in use (amd64 and arm64), downloading what is missing; it sends the agent an `ApplyUpdate` command; the agent fetches `checksums.txt`, its signature and **its own platform's archive** from the hub over its existing mTLS connection (so it needs no internet), checks the signature and the checksum, and leaves a request for the helper of its server (`regulus-agent-update.path`, which the installer enables for the agent role together with `updates: true` in `agent.yaml`; `--no-self-update` skips it). That helper works exactly like the hub's: it checks everything again as root, replaces the binary and restarts the agent. The new agent is healthy when it **connects to the hub again with the new version** (it writes `connected.json` for the helper); if it does not within 90 seconds the previous binary is put back. The hub marks a server `updated` only when it sees it back with the new version, and `failed` if it comes back with the old one (rolled back) or does not come back. An agent installed before this feature has to be updated once with `install.sh`; the dashboard says so. A rollout lasts as long as the hub runs: restarting the hub ends it.
- **Turn it off:** set `updates.check: false` (or `REGULUS_UPDATES_CHECK=false`) on a hub that must not contact the internet. Nothing is requested then, and the page says why.
- **API:** `GET /api/updates`, `PUT /api/updates/settings`, `POST /api/updates/check`, `POST /api/updates/download`, `POST /api/updates/apply`, `GET`/`POST /api/updates/rollout` and `POST /api/updates/rollout/stop` (all need `core.update`; see `internal/hub/openapi.yaml`). The download runs in the background; poll `GET /api/updates`.

### Console and command line

Run `regulus` on the server (or `regulus console`) to open a full-screen console, in the spirit of the Claude Code and Gemini CLIs. It has four sections, reached with `tab`, the arrow keys, `1`-`4` or the mouse:

- **Status:** whether the hub runs (and the state of its systemd unit), version, uptime, the dashboard address and port (with the addresses of this machine on the network), the agent port, the data directory and database size, the configuration file and the connected agents. While no administrator exists it shows the one-time **setup token**, and `p` makes a one-time password reset link for the administrator (after a confirmation), so a server without a browser nearby is still manageable.
- **Settings:** every setting with its description, default, current value and where the value comes from (`default`, `file` or `environment`). `enter` edits one; the text is checked as you type, `tab` cycles through the allowed choices, and `enter` writes the change into the YAML file, keeping its comments. The console says whether the hub must be restarted and offers it with `r` (`sudo systemctl restart regulus`).
- **Logs:** the last lines of the hub's log from journald, refreshed every two seconds, with a level filter on `f`.
- **Health:** the configuration is valid, the hub answers, the ports are free or served by the hub, the data directory is writable, the database exists.

The live data (agents, uptime, setup token) comes from the running hub through a Unix socket in the data directory (`regulus.sock`, mode `0600`, owned by the user that runs the hub), not through the web login. With the package install the data directory is private to `regulus`, so open the console with `sudo -u regulus regulus` to see it all, or with `sudo regulus` (changes to the configuration file need root as well). Without access to the socket the console still shows the settings and tells you what is missing.

Every screen has a scriptable equivalent that uses the same code and the same file:

| Command | What it does |
|---|---|
| `regulus status [--json\|--plain]` | the Status and Health information; exit status `0` when the hub runs, `3` when it does not, `1` when the configuration is broken |
| `regulus config list [--json\|--plain]` | every setting with value and source |
| `regulus config get KEY` | the current value of one setting |
| `regulus config set KEY VALUE` | validates and writes one setting into the configuration file (a bare port such as `9090` becomes `:9090`; `agent_listen off` turns remote agents off) |
| `regulus config path` | the configuration file in use |

The file is `--config`, else `REGULUS_CONFIG`, else `/etc/regulus/config.yaml` when it exists. `config set` changes only that one line (a commented example such as `# agent_listen: ":8443"` becomes the setting) and refuses a value the hub would not accept. The settings are `listen`, `agent_listen`, `data_dir`, `builtin_agent`, `metrics_interval`, `log.level`, `log.format` and `console.tui`.

**Turning the interactive screen off.** It is never forced. `regulus` prints the status as text instead when any of these holds, in this order (a flag beats an environment variable, which beats the configuration file, which beats detection): the flag `--no-tui` (alias `--notui`; `--tui` forces the screen on), the environment (`REGULUS_NO_TUI=1`, `CI` set, `TERM=dumb`), `console.tui: false` in the configuration file, or detection (stdin or stdout is not a terminal, such as a pipe, cron, systemd or SSH without a TTY, or the terminal is smaller than 60x16). `--json` and `--plain` print instead of opening the screen, and `--no-input` (alias `--non-interactive`) means that nothing is ever asked. `NO_COLOR` and `--no-color` only remove the colours; without a UTF-8 locale the console draws with plain ASCII characters. The strings of the screen are in `internal/console/strings.go`, ready for a translation.

Not in the console yet: the reachability from the network, certificate validity and pending updates in Health (they need the update and TLS features), and creating the administrator (the setup token and the dashboard do it).

### Authentication

The hub has local accounts; each has a role, and a role is a set of permissions (see **Roles and permissions** below). Three roles are built in: `admin`, `operator` and `viewer`. On first start, with no accounts, the hub logs a one-time **setup token** (the line "no accounts yet", field `setup_token`); the dashboard asks for it together with the username and password of the first admin, so only someone who can read the hub log can claim an unconfigured hub. A new token is generated at each start until the admin exists.

- Passwords are hashed with argon2id (at least 10 characters). Login creates a session of 7 days stored in the database (only the SHA-256 of the token); the cookie is `HttpOnly`, `SameSite=Lax` and `Secure` when the request came over HTTPS (directly or via `X-Forwarded-Proto`). POST requests must be JSON from the same origin.
- After 10 failed logins from the same client address in 15 minutes, further attempts get `429` for the rest of the window. Behind a reverse proxy all clients share the proxy address, so put rate limiting in the proxy as well.
- Everything under `/api/` except `/api/auth/*` needs a login (`401` otherwise). The dashboard files and `/healthz` are public: they contain no data, and the page shows the login or setup form before anything else.
- Serve the hub over HTTPS (a reverse proxy is fine) outside a trusted network: sessions and passwords travel in the clear otherwise.

Endpoints: `GET /api/auth/me`, `POST /api/auth/setup`, `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/reset`.

**Users, sessions and audit.** Admins manage accounts in the dashboard (**Users**) or through the API: `GET`/`POST /api/users`, `PATCH`/`DELETE /api/users/{username}`. What each user may do depends on the permissions of their role (`403` when missing). The last enabled admin can never be demoted, disabled or deleted. Disabling or deleting an account ends its sessions at once, and a disabled account gets the same "invalid username or password" answer as a wrong password. Every user can change their own password (`POST /api/account/password`, which ends their other sessions and counts wrong current passwords toward the rate limit) and list or end their own sessions (`GET /api/account/sessions`, `DELETE /api/account/sessions/{id}`; each session records the address and browser it was created from). Account events (setup, logins and failed logins, user changes, password changes, ended sessions) go to an audit log in the database, newest 10,000 kept, readable with the `audit.read` permission at `GET /api/audit?limit=` and in the **Users** page; it never stores passwords or tokens.

**Roles and permissions.** Every endpoint that changes or reveals something checks a named permission, not a role name: `servers.read` (see the servers and their metrics), `servers.manage` (enrollment tokens, rename, revoke), `layout.edit` (arrange the dashboard), `users.manage` (accounts, resets, two-factor policy), `roles.manage` (the roles below), `audit.read` and `core.update` (see and steer the [updates](#updates)). The built-in roles live in the code: `admin` has every permission, including the ones later versions add; `operator` has `servers.read`, `servers.manage` and `layout.edit`; `viewer` has `servers.read` and `layout.edit`. Anyone with `roles.manage` can make **custom roles** per installation in **Users → Roles** (`GET`/`POST /api/roles`, `PATCH`/`DELETE /api/roles/{name}`, audited): pick a name (lowercase letters, digits, `-` and `_`) and tick permissions. A role in use cannot be deleted, and the built-in ones cannot be changed. Nobody can hand out more than they hold: a role or an account can only be given permissions its author has, and an account can only be changed, disabled, deleted or reset by someone whose permissions cover all of that account's. Changes to a role apply at the next request, with no new login. The signed-in user's permissions are in `GET /api/auth/me`. The OS-level privileges of the services are a separate matter (see the [design decisions](docs/decisions.md#updates) and #46).

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

How it is protected: the hub creates its own certificate authority at the first start (`pki/` in the data directory, `ca.key` readable by the owner only). Tokens are random, stored only as a hash, single use, expire after an hour and are rate limited (10 wrong attempts per 15 minutes per address). Every connection needs the client certificate of an enrolled, not revoked agent, and the agent id comes from the certificate, not from what the agent says. Client certificates last five years; there is no renewal yet. Creating tokens, renaming and revoking agents need `servers.manage` and go to the audit log.

**Versions.** An agent and the hub agree on a protocol version when the agent connects (the agent sends the range it speaks, the hub picks the highest both understand). On top of that the hub accepts agents of **its own release series and the previous one**: the same minor version, one minor behind (hub 0.3.x takes 0.3.x and 0.2.x), or any release of the previous major. Updating the hub first is therefore safe: its agents keep working until they are updated too. The **Servers** page marks agents that are behind the hub, or ahead of it (update the hub). An agent further behind is refused with a message that says so and the server is listed as `incompatible` with the reason. A refusal because of versions does not stop the agent service: it asks again every few seconds, so it connects by itself once the agent or the hub is updated. A revoked agent still stops for good. Development builds are never judged.

### HTTP API

`GET /api/agents` returns the connected agents as JSON (id, version, system info, `status` of `connected` or `stale`, last heartbeat, uptime). `GET /api/agents/{id}/metrics` returns the recent metrics of an agent (CPU, load, memory, swap, disks, network counters and rates, uptime), oldest first; `?since=<RFC 3339>` returns only newer points. The hub keeps the last hour of metrics in memory (up to 3600 reports per agent), so the history is lost when the hub restarts. `GET /api/agents/{id}` returns one agent. `GET /api/agents?all=true` also lists the enrolled agents that are `offline` or `revoked`, with the time the hub last heard from them (`last_seen_at`); `PATCH /api/agents/{id}` (`servers.manage`) gives an agent a display name. `GET /api/layout` and `PUT /api/layout` read and save the dashboard layout of the logged-in user (any JSON up to 64 KiB, stored per user). `GET /api/stream` upgrades to a **WebSocket** that pushes a `snapshot` of the agents and then `agent.connected`, `agent.disconnected`, `agent.heartbeat` and `agent.metrics` events as JSON; it needs the session cookie and refuses pages from other origins, and a client that reads too slowly is disconnected and should reconnect. All of this is described in [`internal/hub/openapi.yaml`](internal/hub/openapi.yaml), also served at `/api/openapi.yaml`; a test keeps the file in sync with the routes. The endpoints need a login and `GET`s are read-only; the dashboard shows the servers on the **Servers** page.

### Dashboard

After the login the dashboard shows a header with the sections **Dashboard**, **Servers**, (for those who may manage users or roles or read the audit log) **Users** and (with `core.update`) **Updates**, and a link to your **Account** page (change password, your sessions). The **Dashboard** page is a grid of widgets: **Customize** unlocks dragging and resizing, **Add widget** picks from the registered widgets, and **Reset to default** restores the initial layout. The layout is saved per user through `/api/layout` (with debouncing, and flushed when leaving the page). Widgets are registered in `dashboard/src/widgets/` with `registerWidget` (id, title, default size, component); a widget that crashes only breaks its own frame, and a layout that refers to an unknown widget (for example from a removed plugin) shows a placeholder that can be removed.

The base installation ships these widgets, fed live by the hub (the dashboard follows `/api/stream` and reconnects by itself if the connection drops): **System** (host, OS, kernel, memory, load, uptime), **CPU** and **Memory** (current value plus a chart of the last 10 minutes), **Network** (received and sent rate, in total and per interface), **Disk usage** (one bar per mounted filesystem), **Uptime**, and **Servers**. The first five (without Uptime) are in the default layout. When several agents are connected, each widget has a selector and remembers the choice; by default it shows the hub's built-in agent.

The **Servers** page lists every enrolled server, online or not, with its system, agent version, status (online, stale, offline, revoked) and last contact. A row opens the page of that server: its facts and the System, CPU, Memory, Network and Disk usage widgets bound to it. Those with `servers.manage` rename or revoke a server there and add new ones with **Add a server**. An offline server shows when it was last seen; a revoked one cannot connect again.

## Repository layout

```
cmd/regulus/       single executable (role chosen with a subcommand)
internal/config/   configuration (defaults, YAML file, environment, flags) and the editable settings
internal/console/  the interactive console (Bubble Tea) and the status and settings output
internal/localstatus/ the Unix socket where a running hub reports to local tools
internal/logging/  structured logging
internal/storage/  SQLite database and migrations
internal/auth/     local accounts, sessions and the HTTP login endpoints
internal/hub/      hub: HTTP server, agents API and the agent service (registry of connected agents)
internal/update/   looking for new releases, the download cache and its checks, installing with a rollback (hub and agent)
internal/agent/    agent runtime (client side of the contract) and Linux metrics collector, also used as the built-in agent
internal/protocol/ protocol version of the agent-hub contract
internal/inproc/   in-memory gRPC transport (used by the built-in agent)
internal/gen/      generated Go code for proto/ (do not edit)
internal/webui/    embeds the compiled dashboard
dashboard/         React + TypeScript dashboard
packaging/         systemd unit, default config, maintainer scripts and install.sh
proto/             Protobuf contract between agents and the hub
```

Also: `docs/` (design decisions and the plugin design draft), `AGENTS.md` (notes for AI coding agents). Planned: `plugins/` (internal plugins).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and the [design decisions](docs/decisions.md). Security problems: [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
