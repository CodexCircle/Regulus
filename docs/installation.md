# Installation (Ubuntu Server)

The hub installs on Ubuntu Server 22.04, 24.04 and 26.04 (amd64 and arm64; other Debian-based systems with systemd should work too). One command downloads the `.deb` of the latest release, verifies its checksum and signature, installs it, starts the service and prints where to open the dashboard and the one-time setup token:

```sh
curl -fsSLO https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh
sh install.sh
```

The same in one line is `curl -fsSL …/install.sh | sh`, with options after `sh -s --`; if the `-s` gets lost, `sh` takes the first option for a file name ("cannot open --version"), so the two steps are safer. Until the first stable release is out, `latest` has nothing to serve: use the command at the top of the notes of a [pre-release](https://github.com/CodexCircle/Regulus/releases).

The installer asks whether to include the **built-in agent** (so the hub's own server is managed like the others). To answer in advance, or in a script, add options after `sh install.sh` (or `sh -s --` in the one-line form): `--builtin-agent`, `--no-builtin-agent`, `--yes` (no questions), `--channel beta` or `--channel alpha` (latest release of that channel; also `--beta`, `--alpha`), `--version 0.2.0`, `--require-signature` (fail unless the signature can be verified with [cosign](https://docs.sigstore.dev/cosign/)). Without cosign the installer verifies the checksum only and says so. If you prefer not to pipe a script into a shell, download the `.deb` from the [releases](https://github.com/CodexCircle/Regulus/releases), verify it as shown under [Releases](releases.md) and run `sudo apt install ./regulus_*.deb`.

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
curl -fsSLO https://github.com/CodexCircle/Regulus/releases/download/vVERSION/install.sh && sh install.sh --version VERSION --role agent --hub HUB_HOST:8443 --token TOKEN --ca-fingerprint sha256:...
```

It verifies and installs the same package, enrolls the server as the `regulus` user (the token is sent only to a hub whose CA has that fingerprint), then enables and starts `regulus-agent.service`; the server shows as online within seconds. The hub must have the agent listener on (see [Remote agents](remote-agents.md)). `VERSION` is the version the hub runs: the dashboard fills it in, so the agent matches the hub even when only pre-releases exist. Keep the token off the command line with `REGULUS_AGENT_TOKEN=... sh install.sh --role agent ...`. If the hub revokes the agent, the service stops and is not restarted (exit status 3).

The dashboard speaks plain HTTP on port 8080. Before exposing the hub beyond a trusted network, put a reverse proxy with TLS in front of it and set `listen: 127.0.0.1:8080` in `/etc/regulus/config.yaml`.
