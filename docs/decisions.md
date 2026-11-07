# Design decisions

The choices behind Regulus that the code alone does not explain: what was decided, why, and what is still open. The README says *how* things work; this file says *why they are that way*. When a decision changes, update this file in the same pull request.

## Tech stack: Go and React

Hub and agent are written in **Go**, the dashboard in **React with TypeScript**. The alternatives considered were: hub in C# with the agent in Go, everything in C#, Rust, and Node.js.

Go was chosen because:

- the agent must stay light even on a small VPS (512 MB of RAM);
- it builds a **single executable with no runtime** to install, so one package serves both roles;
- the agent opens an **outbound** connection to the hub, and Go's gRPC support handles that well;
- plugins run as **separate processes speaking gRPC**, so they are not tied to Go: a plugin can be written in C# or any other language with gRPC.

Any memory or size figures quoted while choosing were orders of magnitude, not measurements.

## License: Apache-2.0

Apache-2.0 was preferred to AGPL-3.0. A permissive license makes it easier for others to write and distribute plugins (#23); AGPL would protect the project more but would discourage third-party plugins. Changing the license after outside contributions is hard, so this choice is tied to the plugin strategy.

## Platforms

Ubuntu Server comes first because it is the system used for the first real tests. The order is:

1. Ubuntu LTS 22.04, 24.04 and 26.04, amd64 and arm64, `.deb` package (26.04 amd64 is the one tested on a real machine so far, #81; whether 22.04 stays is open in #89);
2. Debian, with the same `.deb`;
3. RHEL-like distributions, `.rpm` package.

Other distributions with systemd can use the `tar.gz` archive. An apt repository or PPA for Ubuntu is postponed.

## Roadmap: phases and milestones

The README describes four **phases**; the work is tracked in six **milestones**, M1 to M6. Issue titles still carry the phase they were first written for (`[Phase 1]`, …), so a title and a milestone can disagree: the milestone is what counts.

| Milestone | Content | Phase |
|---|---|---|
| M1 | Foundations: skeleton, hub core, agent-hub contract, CI, release workflow | 1 |
| M2 | Hub with built-in agent and dashboard: authentication, users, widgets, Ubuntu packaging | 1 |
| M3 | Remote agents: enrollment, multi-server dashboard, agent installer | 2 |
| M4 | Separate hub and agent, protocol versioning | 3 |
| M5 | Update system | 3 |
| M6 | Plugins | 4 |

Some issues were moved after they were written, so their title names an earlier phase:

- **Two-factor authentication (#49) in M3**: it should exist before one dashboard manages several servers.
- **Passkeys (#50) in M4**: WebAuthn needs HTTPS and a public domain (`public_url`), which come with the separate hub.
- **User management (#48) and credential reset (#51) in M2**: without a reset, an administrator who is locked out cannot get back in.
- **Roles (#61) and the terminal console (#52) in M3**; **privileged actions (#60) and server inventory (#62) in M4**; **AI Support (#63) in M6**.

Third-party plugins (#23) have no milestone: they come after the plugin system is stable.

## Releases and channels

Three channels: **stable** (`v0.3.0`), **beta** (`v0.3.0-beta.N`; `-rc.N` counts as beta) and **alpha** (`v0.3.0-alpha.N`; any other pre-release counts as alpha, so an unknown kind never reaches a more cautious channel). Each channel also offers the more stable ones: alpha sees everything, beta sees betas and releases, stable only releases. A hub starts on the channel of the version it runs.

Releases are published from the Actions tab (**Publish release**), not by typing tags, so that versions always follow SemVer order: a version goes through its alphas, then its betas, then the release, and no alpha comes after a beta of the same version. The workflow refuses a commit on which CI has not passed. It must start the release workflow **on the tag**, because the hub only trusts a signature made by `release.yml@refs/tags/<tag>`.

## Updates

**Goal: install once.** After the first installation, an administrator should never have to install anything by hand from a terminal: hub, agents and (later) plugins are updated from the dashboard. Today this holds for hub and agents. Still missing: plugin updates (#19, #20) and two exceptions [updates](updates.md) lists (an agent installed before the update feature, and a release that changes the systemd units or the configuration file).

**One piece at a time.** It must be possible to update an agent, the hub or a plugin on its own. Versions may require others: agents and hub negotiate the protocol and the hub accepts agents of its own release series and the previous one (#17, [remote agents](remote-agents.md), "Versions"); plugins declare the core versions they need with `requires` (#19, [plugins](plugins.md)).

**Who decides.** The administrator chooses the mode in the dashboard: `manual` (tell me), `download` (download and verify, then wait for my approval) or `automatic` (install inside a maintenance window). The default for a new installation is **`download`**: nothing is installed without an administrator's approval.

**How the privileged step works.** The hub runs as the unprivileged user `regulus`. Installing a new binary is done by a root helper started by a systemd path unit (`regulus-update.path`, `regulus-agent-update.path`), which the installer enables unless `--no-self-update` is given. The helper acts only on a request naming a version, and checks everything again itself. Releases are signed with **Sigstore keyless** (GitHub Actions identity). An earlier design used a `sudoers.d` rule and minisign or cosign keys and disabled self-update for package installations; that design was replaced by the current one. Whether `sudoers.d` or a root helper is better for the other privileged actions (services, packages, Docker) is still open in #60.

## Remote agents from the dashboard

Remote agents can be turned on from the dashboard (#90), so that enrolling a second server needs no terminal on the hub. The choice is kept in the database, because the hub runs as `regulus` and cannot write `/etc/regulus/config.yaml`. `agent_listen` in the configuration always wins: when it is set the dashboard shows it and cannot change it. Turning the listener on needs `servers.manage`, the same permission as making enrollment tokens, since one is useless without the other; it is audited. Turning it off from the dashboard is not offered yet.

## Verification status

No release has been published yet, and several parts have only been tested by unit and integration tests: the signed release, the `.deb` on a clean Ubuntu system, the agent installer on a second server, and installing and rolling back an update with real systemd. They will be checked together on a VM (#81). Until then, try the first release on a test server.

## Language: English only, GUI in English and Italian

Decided by the maintainer on 2026-10-29 (#120). Code, comments, commits, issues, pull requests and documents are in English only. The GUI (dashboard and console) supports English and Italian in every version, alpha included. The Italian sections that used to follow the English text in the README, `CONTRIBUTING.md`, `SECURITY.md`, this file and [plugins](plugins.md) were removed: one source of truth, no double maintenance, no drift. The Italian text stays in git history. Issues opened before this decision stay bilingual. There is no translation mechanism for the GUI yet (the console has an English-only string table, the dashboard hard-codes its strings): #125.

## Database: SQLite by default, PostgreSQL by configuration

Done in #122. Choices, so they are not rediscovered:

- **Driver:** `github.com/jackc/pgx/v5` through its `database/sql` adapter. Pure Go (the binary stays static, no cgo), MIT licence (compatible with Apache-2.0). It was already in the module graph as an indirect dependency.
- **One place knows the dialect.** Option (a) of the issue: **separate migration directories per backend** (`migrations/sqlite`, `migrations/postgres`, same numbers and names) with a test that compares the resulting schema, plus a small **`Rebind`** in the PostgreSQL connection wrapper that turns `?` into `$n`, so none of the 80 call sites changed. Rejected: one portable SQL set plus a rewriter (SQLite `STRICT`, `INTEGER PRIMARY KEY`, `COLLATE NOCASE` and the table rebuild of migration 9 cannot be written once).
- **Portable SQL in the code:** `INSERT OR REPLACE` became `INSERT ... ON CONFLICT (user_id) DO UPDATE`; `LastInsertId` (not supported by pgx) became `INSERT ... RETURNING id`; unique violations are recognised by `storage.IsUniqueViolation` on both; parameters in the select list of an `INSERT ... SELECT` are cast (`CAST(? AS TEXT)`) because PostgreSQL cannot infer their type.
- **Time and value representation:** unchanged. Timestamps stay ISO-8601 text and flags stay integers in PostgreSQL too, so every code path is identical on both backends. Row ids are `BIGINT` identity columns. Cost: no `timestamptz` arithmetic in SQL, and a later move to native types is a migration of its own.
- **Case-insensitive names** (`COLLATE NOCASE` in SQLite) use the **`citext`** extension in PostgreSQL, created in `public` with `public` kept on the search path. Alternatives rejected: nondeterministic ICU collations (not usable in foreign keys before PostgreSQL 18), and `lower()` indexes (would need every query and the `roles` foreign key changed).
- **Migrations on PostgreSQL:** transactional DDL; the same "refuse a newer schema" rule; a session **advisory lock** around the whole run. The `-- regulus:no-foreign-keys` mechanism is **not** needed there (the constraint is dropped in place), so it is ignored on PostgreSQL.
- **Connections:** SQLite keeps one connection. PostgreSQL uses a pool (10 open, 4 idle, 30 min lifetime, 5 min idle) and waits up to 60 s for the server at start. A running hub does not exit when the database goes away; it logs the change of state once and reports it in `regulus status` and the console (see the gap list: not in the dashboard yet).
- **Password:** environment only (`REGULUS_DATABASE_PASSWORD`, normally `/etc/regulus/regulus.env`, `0640 root:regulus`). It is deliberately **not** a YAML key, so `config.yaml` (which `regulus config set` rewrites and which may be copied around) can never hold it and the strict YAML parser rejects it. Shown as `********` when set.
- **Open questions of the issue, answered:** minimum PostgreSQL **14** (Ubuntu 22.04; tests run on 16); default `sslmode` is **`prefer`** (works for a local server; `verify-full` plus `database.sslrootcert` for a remote one); schema is **`public` by default**, configurable with `database.schema` (which must exist).
- **Check command:** `regulus db check` (name confirmed here), see [Database](database.md).
- **CI:** a PostgreSQL service job runs the whole suite with `REGULUS_TEST_DATABASE_URL`; each test gets its own schema (`storagetest`).

## Open questions

- Should the other privileged actions use a `sudoers.d` rule or a root helper like the updates? (#60)
- Plugin dependencies, signatures and layout: see the open questions in [plugins](plugins.md).
