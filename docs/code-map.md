# Reading map for the largest source files

A map of the biggest Go files (without tests), so a newcomer can find their way before reading them in full. Size alone is not a defect, so each file ends with a decision: leave as is, or a split proposal. For the whole system see [architecture.md](architecture.md); for the package-level view start with each package's comment (`go doc ./internal/hub`).

Sizes are lines on `main` on 2026-10-29. Line numbers are approximate and drift; the function names are the stable part. Each file also starts with a short comment that says the same in a few lines.

| File | Lines | Decision |
|---|---|---|
| [internal/update/manager.go](../internal/update/manager.go) | 852 | leave |
| [internal/console/tui.go](../internal/console/tui.go) | 744 | leave |
| [internal/auth/twofactor.go](../internal/auth/twofactor.go) | 536 | leave |
| [internal/hub/hub.go](../internal/hub/hub.go) | 481 | split proposed (routes) |
| [internal/hub/rollout.go](../internal/hub/rollout.go) | 485 | split proposed (`FetchUpdate`) |
| [internal/hub/agents.go](../internal/hub/agents.go) | 487 | leave |
| [internal/update/installer.go](../internal/update/installer.go) | 438 | leave |
| [cmd/regulus/console.go](../cmd/regulus/console.go) | 424 | split proposed (`config`, reset helpers) |

## internal/update/manager.go

**Responsibility.** The hub's side of updates: it decides when to look for a release, keeps the newest verified one in a cache, and asks the root helper to install it. It does not verify signatures (`signature.go`, `stage.go`), talk to GitHub (`source.go`) or install anything (`installer.go`, run as root).

**Reading order.**
1. Types an administrator sees: `Mode`, `Settings` (with `Validate`, `InWindow`), `Info`, `Status` (about lines 16 to 155).
2. Constants for timing and the keys saved in the `settings` table (about 175 to 198), then `Options` and `Manager`.
3. `New`, then `Run` with `firstDelay`, `cycle` and `automatic`: the periodic loop.
4. The API entry points: `Settings`, `SetSettings`, `Status`, `Check`, `Download`, `Apply`.
5. Internals: `check`, `download`, `stage`, `doStage`, `failed`, the persistence helpers (`get`, `put`, `load`, `saveState`), `applyStatus` and `syncApply` (reading the helper's result).
6. `EnsureRelease` and `Files`: what the hub's rollout to agents uses (`internal/hub/rollout.go`).

**Entry points.** `update.New`, `Manager.Run`; the methods called from `internal/hub/updates_api.go`.
**Depends on.** `Source`, `Verifier`/`Stager`, `ApplyDir` request files (`apply.go`), the database, an `Auditor` (`*auth.Service`).
**Decision: leave.** Many functions, one state machine around shared fields; splitting would spread locking across files for little gain. The types at the top could move to their own file later; that is style, not a second responsibility.

## internal/console/tui.go

**Responsibility.** The Bubble Tea model of the interactive console. It does not collect the data (`snapshot.go`), hold the strings (`strings.go`) or print plain text (`text.go`).

**Reading order.** `tab`, `Hooks`, `Options`, `Model`; `New`, `Run`; messages; `Init`, `tick`, `collect`, `loadLogs`; `Update` with its helpers `key`, `mouse`, `edit`, `answer`, `startEdit`; `View` with `footer`, `window`, and one body per tab: `statusBody`, `settingsBody`, `logsBody`, `healthBody`; log filtering (`logSeverity`, `filterLogs`).

**Entry points.** `console.Run`, `console.New`. **Depends on.** `internal/config` (the editable settings).
**Decision: leave.** One model; the render functions read the model's fields all the time, so a split would pass the model around without separating a concern.

## internal/auth/twofactor.go

**Responsibility.** The two-factor (TOTP) part of `Service`. It does not do the arithmetic (`totp.go`), the database access (`store_twofactor.go`) or the HTTP routes (`twofactor_http.go`).

**Reading order.** Constants and errors; `Policy` with `SetPolicy`, `Mandatory`, `MustEnroll`; `TwoFactorStatus`; setup (`BeginTwoFactor`, `EnableTwoFactor`, `newRecoveryCodes`); the second-factor check (`checkSecondFactor`, `verifyRecoverable`); management (`DisableTwoFactor`, `RenewRecoveryCodes`, `ResetTwoFactor`); the second step of sign-in (`beginPending`, `CompleteLogin`).

**Entry points.** The exported methods on `Service`, called from `twofactor_http.go` and `http.go`. **Depends on.** the secret box (`secret.go`), `store`.
**Decision: leave.** Policy, setup and sign-in are three phases of one feature that share `checkSecondFactor` and the rate-limit helpers.

## internal/hub/hub.go

**Responsibility.** Building and running the `Hub` (`New`, `Serve`, the listener for remote agents, the built-in agent), and at the end the HTTP route table. It does not implement the agent protocol (`agents.go`), enrollment (`fleet_api.go`) or the rollout (`rollout.go`).

**Reading order.** `Hub`, `Option` and the `With…` options; `New`; the accessors the API uses (`Agents`, `Metrics`, `SendCommand`, `AgentConn`); `Serve`; `serveAgents`; `startBuiltinAgent`; then `Deps`, `route`, `apiRoutes` and `NewHandler` (about lines 366 to 481).

**Entry points.** `hub.New`, `Hub.Serve`, `hub.NewHandler`. **Depends on.** `agent`, `auth`, `config`, `fleet`, `inproc`, `pki`, `storage`, `update` and the generated contract.
**Decision: split proposed.** Two responsibilities: *lifecycle* (everything up to `startBuiltinAgent`) and *HTTP routing* (`Deps`, `route`, `apiRoutes`, `NewHandler`, about 115 lines). Proposal: move the routing block unchanged to `internal/hub/routes.go`; `hub.go` keeps the lifecycle. **Not before the implementation work of #103 has settled in `internal/hub`.**

## internal/hub/rollout.go

**Responsibility.** Updating connected agents to the hub's version in waves, stopping at the first failure. It does not install anything on the hub itself (`internal/update`).

**Reading order.** Errors and audit events; `RolloutAgent`, `RolloutStatus`, `RolloutOptions`, `RolloutControl`; `rolloutState`; `candidates`; `StartRollout`, `StopRollout`; `runRollout` and `updateAgent` (offer, wait for the agent to come back); `autoRollout` (the automatic mode); **and, at the end, `agentServer.FetchUpdate`**, which streams release files to an agent.

**Entry points.** `Hub.StartRollout`, `Hub.StopRollout`, `Hub.RolloutStatus`, `autoRollout` (called from `Serve`'s timer); `FetchUpdate` is called by agents.
**Depends on.** the generated contract and `internal/update`.
**Decision: split proposed.** `FetchUpdate` (about 55 lines) is the *server side of an RPC on `agentServer`*, not part of the rollout; it belongs next to the rest of `AgentService`. Proposal: move it unchanged to `internal/hub/agent_files.go`. **Not before the implementation work of #103 has settled in `internal/hub`.**

## internal/hub/agents.go

**Responsibility.** The hub's implementation of `AgentService` (`agentServer`): handshake, registry of live sessions, heartbeats, metrics, commands to agents. It does not authenticate agents (`agent_net.go`) or decide about updates.

**Reading order.** `AgentInfo`, `Rejection`, `agentServer`, `session`; `Connect` (handshake, protocol and version checks) with `readHello`; `serve` and `handle`; `register`, `unregister`, `disconnect`; the queries (`agents`, `metricsHistory`); `sendCommand` with the `session` pending-result helpers; `reject`.

**Entry points.** `Connect` (gRPC), and the methods `Hub` forwards to. **Depends on.** the generated contract, `internal/protocol`, `internal/update` (`CheckCompat`).
**Decision: leave.** It is one object with one lifecycle; the helpers on `session` are small.

## internal/update/installer.go

**Responsibility.** The root helper behind `regulus update-apply`: re-verify, replace the binary, restart, watch health, roll back. It does not decide *whether* to update (that is `manager.go`).

**Reading order.** `Runner`; `Installer` and `defaults` (the 90 s and 15 s health settings); `Apply`; `fetchVerified`; `restartAndWatch`; `rollback`; the safe file helpers (`openRegular`, `readRegular`, `extractBinary`, `copyExecutable`).

**Entry points.** `Installer.Apply`, called from `cmd/regulus/update_apply.go`. **Depends on.** `localstatus`, the verifier, systemd (through `Runner`).
**Decision: leave.** One linear procedure; the file helpers are part of why the procedure is safe and are read together with it.

## cmd/regulus/console.go

**Responsibility.** The commands built on the console code: `regulus` / `regulus console`, `regulus status`, and `regulus config`, plus what they share. The `hub` command is in `main.go`, `agent` in `agent.go`, `hub reset-admin` in `reset_admin.go`.

**Reading order.** `terminalInfo` and `detectTerminal`; `env` and `hostEnv`; `commonFlags`; `consoleCommand` (console and status); `hostHooks` and the reset-admin helpers (`resetAdminLink`, `ownerOf`, `resetAdminAs`) and `restartService`; `configCommand`.

**Entry points.** `runConsole`, `runStatus`, `runConfig`, called from `main.go`. **Depends on.** `internal/config`, `internal/console`.
**Decision: split proposed.** Three responsibilities share the file: the console/status command, the `config` command (`runConfig`, `configCommand`, about 90 lines), and helpers for resetting the administrator that overlap in purpose with `reset_admin.go`. Proposal: move the `config` command to `cmd/regulus/config.go` unchanged. Moving the reset helpers next to `reset_admin.go` is a second, optional step. This directory is not touched by #103, so it can go first.

## Splits: rules

A split, when approved, is its own pull request per file: **no behaviour change, a pure move**, and the existing tests pass unchanged. Nothing in `internal/hub`, `internal/agent` or `internal/pki` is split while #103 is being implemented, to avoid merge conflicts.
