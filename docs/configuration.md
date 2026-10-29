# Configuration

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
