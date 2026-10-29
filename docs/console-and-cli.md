# Console and command line

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
