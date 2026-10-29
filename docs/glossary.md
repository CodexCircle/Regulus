# Glossary

What each word means in Regulus **today**. The names are provisional: the maintainer will decide the final nomenclature later (see [the naming plan](#naming-plan)). Until then, new text uses the words defined here, and nobody invents a new name without adding it to this page.

Status of a term: **in use** (the code or the UI uses it), **provisional** (used, but the name may change), **proposed in #103** (only in the design issues of the epic for hub promotion and failover, no code yet), **deprecated**. For how the parts fit together see [architecture.md](architecture.md).

## Words with more than one meaning

These cause the most confusion. Say which one you mean.

| Word | Meaning 1 | Meaning 2 | Meaning 3 |
|---|---|---|---|
| **role** | *Node role*: what a Regulus process does on a machine, `hub` or `agent` (chosen by the subcommand `regulus hub` / `regulus agent`; installer option `--role`). Proposed in #104: more states such as redundant hub and pure-hub. | *User role*: a named set of permissions given to a person (`admin`, `operator`, `viewer`, custom ones). Code: `internal/auth`. | |
| **channel** | *Release channel*: `stable`, `beta` or `alpha`, chosen on the Updates page. Code: `internal/update`. | *Channel between hubs*: the secure link between the active and the redundant hub, proposed in #107. | *Out-of-band channel*: email, Telegram… to reach the administrator when the hubs cannot, proposed in #112. |
| **node** | Not used in the code, the UI or the current docs. Only the design issues of #103 use it, for "any machine in the group: agent, hub, candidate". | | |
| **hub** | The process (`regulus hub`). | The machine that runs it. | In #103: the *active* hub or a *redundant* hub; "the hub" then means the active one. |
| **server** | The UI word for a managed machine (**Servers** page, **Add a server**). | A *server certificate* or the *hub's HTTP server* in technical text. Not the same as the hub. | |
| **token** | *Enrollment token*: single-use, one hour, lets a new agent join. | *Setup token*: one-time, logged at first start, lets the first admin be created. | *Reset token* (password reset link) and the proposed *promotion token* (#105). Session tokens exist too but are never shown. |

## Terms

Alphabetical. "Closest other term" says what it is most often confused with.

| Term | Definition | Status | Where it appears | Closest other term | Italian UI label (proposed) |
|---|---|---|---|---|---|
| **active hub** | The one hub that currently orchestrates the group; exactly one per term. | proposed in #103 | #103, #104, #108 | *hub* (today there is only one) | hub attivo |
| **admin** | The built-in user role with every permission, including ones added later. | in use | `internal/auth/permissions.go`; **Users** page | *administrator* (a person) | amministratore |
| **agent** | A `regulus agent` process on a managed server. It reports the machine to a hub and runs the hub's commands. Remote agents connect outbound to the hub. | in use | `internal/agent`; `regulus agent`; `/api/agents` | *server* (the machine), *built-in agent* | agente |
| **agent id** | Identifier the hub gives an agent at enrollment; also the common name of its certificate. `local` is the built-in agent's id. | in use | `internal/fleet`, `proto` (`agent_id`) | *server name* (a display name set by an admin) | ID agente |
| **audit log** | Record of security-relevant events (logins, user changes, enrollments, updates) in the database. | in use | `internal/auth`; **Users** page; `GET /api/audit` | application log (journald) | registro di controllo |
| **built-in agent** | The agent that runs inside the hub process (agent id `local`) over the in-process transport, so the hub's own server is managed like the others. Setting `builtin_agent`. | in use | `internal/hub/hub.go`, `internal/inproc` | *remote agent* | agente integrato |
| **CA (certificate authority)** | The hub's own authority that signs agent certificates. Created at the first start in `<data_dir>/pki/`. | in use | `internal/pki` | a public CA (not used) | CA (autorità di certificazione) |
| **candidate** | An agent that asked to be promoted and waits, inactive, for the administrator's decision. | proposed in #103 | #104, #105 | *agent* | candidato |
| **channel** | See the table above. Release channel: `stable`, `beta`, `alpha`. | in use | `internal/update`; **Updates** page | the channel between hubs | canale |
| **console** | The full-screen terminal interface, `regulus` or `regulus console`, with Status, Settings, Logs and Health tabs. Also called TUI. | in use | `internal/console`, `cmd/regulus/console.go` | *dashboard* | console |
| **dashboard** | The web interface: widgets arranged per user. | in use | `dashboard/`, `internal/webui` | *console* | dashboard (pannello) |
| **data directory** | `data_dir`: where the hub keeps the database, `secret.key`, `pki/` and the local socket (`/var/lib/regulus` with the package). Agents have their own (`/var/lib/regulus-agent`). | in use | `internal/config` | the configuration directory `/etc/regulus` | directory dei dati |
| **enrollment** | The one-time act of an agent joining a hub: it proves a token, sends a certificate request and receives a client certificate. | in use | `internal/fleet`, `internal/agent/remote.go`, `EnrollmentService` | *connecting* (done on every start) | registrazione (enrollment) |
| **enrollment token** | A random, single-use token valid for one hour, made by a user with `servers.manage`; only its hash is stored. | in use | `fleet.CreateToken`; `POST /api/enrollment-tokens`; **Add a server** | *setup token*, *reset token* | token di registrazione |
| **failback** | Control returning to the old primary after a failover. What it means exactly is not settled. | proposed in #103 | #110 | *failover* | ripristino (failback) |
| **failover** | A redundant hub becoming the active hub because the active one is unreachable. | proposed in #103 | #108, #109 | *promotion* | passaggio al sistema di riserva (failover) |
| **fingerprint pinning** | An agent accepts a hub only if the hub's certificate chain contains a CA with a known SHA-256 fingerprint (`sha256:…`). | in use | `pki.PinnedClientTLS`; `--ca-fingerprint` | normal certificate validation (not used) | impronta della CA |
| **fleet** | Provisional name for the set of remote agents a hub manages, and for the Go package that keeps their tokens and registry. Not used in the UI. | provisional | `internal/fleet` | *group* (#103 text) | flotta |
| **heartbeat** | A periodic message from the agent to the hub (15 s by default) that shows it is alive. | in use | `proto` `Heartbeat`; `internal/hub/agents.go` | *metrics* | heartbeat |
| **hub** | The Regulus process every agent talks to; serves the dashboard and the API. One per group today. | in use | `internal/hub`, `regulus hub` | *server* | hub |
| **local** | The agent id of the built-in agent. | in use | `hub.BuiltinAgentID` | | locale |
| **mutual TLS (mTLS)** | Both sides present certificates: the hub's (checked by pinning) and the agent's (checked against the CA and the registry). | in use | `internal/pki` | | TLS reciproco (mTLS) |
| **node** | Not used today. In #103: any machine in the group. | proposed in #103 | #103 | *server*, *agent* | nodo |
| **operator**, **viewer** | Built-in user roles. `operator`: see and manage servers and arrange the dashboard. `viewer`: see servers and arrange the dashboard. | in use | `internal/auth/permissions.go` | | operatore, osservatore |
| **permission** | A named right, for example `servers.read`, `servers.manage`, `core.update`. Endpoints check permissions, not role names. | in use | `internal/auth` | *user role* | permesso |
| **plugin** | An extension that adds server-side features and dashboard widgets, running as a separate process. | provisional (design only) | `docs/plugins.md`; #19 to #23 | *widget* | plugin |
| **promotion** | Turning an agent into a hub (or a redundant hub), requested on the candidate and approved on the active hub with a token. | proposed in #103 | #105 | *failover* | promozione |
| **protocol version** | The version of the agent–hub contract (`protocol.Version`), negotiated when an agent connects. Separate from the release version. | in use | `internal/protocol` | *release version* | versione del protocollo |
| **pure-hub** | A hub that orchestrates and hosts no workloads, for small machines. Probably "hub with the built-in agent off" plus a rule; not confirmed. | proposed in #103 | #104, #111 | *hub only* setup | hub puro |
| **redundant hub** (also *standby*, *backup hub*) | A passive hub that receives a copy of the active hub's data and takes over if the active one is unreachable. | proposed in #103 | #101, #104, #106 | *backup hub* in #101 (same idea) | hub ridondante |
| **release series** | The same minor version (for example 0.3.x). A hub accepts agents of its own series and the previous one (N / N-1). | in use | `internal/update/compat.go` | *protocol version* | serie di rilascio |
| **revocation** | An admin makes an enrolled agent unable to connect again; the hub closes its connection. | in use | `fleet.Revoke`; `DELETE /api/agents/{id}` | *deleting* a user | revoca |
| **rollout** (and **wave**) | Updating the connected agents to the hub's version a few at a time (a wave of 1, 3, 10 or all), stopping at the first failure. | in use | `internal/hub/rollout.go`; **Updates** page | the hub's own update | aggiornamento dei server (ondata) |
| **setup token** | A one-time token logged by a hub that has no accounts; needed to create the first admin. | in use | `internal/auth`; console Status | *enrollment token* | token di configurazione iniziale |
| **server** | In the UI and user text: a machine managed from the hub. | in use | **Servers** page, **Add a server** | *agent* (the process), *hub* | server |
| **term (epoch)** | A number raised on every change of the active hub; nodes accept orders only from the highest term they have seen. | proposed in #103 | #108 | *protocol version* | termine (epoca) |
| **trust root** | The key material that lets every node check that a hub is legitimate, even after the hub changed. | proposed in #103 | #107 | *CA* (today the CA is the trust root, implicitly) | radice di fiducia |
| **update mode** | `manual` (tell me), `download` (download and verify, then wait; the default) or `automatic` (install in a maintenance window). | in use | `internal/update` `Mode`; **Updates** page | | modalità di aggiornamento |
| **update helper** | The small root process (`regulus update-apply`) started by a systemd path unit that installs a downloaded release and rolls back on failure. | in use | `internal/update/apply.go`, `installer.go`; `packaging/` | the hub itself (unprivileged) | helper di aggiornamento |
| **widget** | A tile of the dashboard (System, CPU, Memory…). | in use | `dashboard/src/widgets/` | *plugin* | widget |

### Server states shown in the UI

`connected` (online), `stale` (silent for two heartbeats), `offline`, `revoked`, and `incompatible` (refused because of its version). Source: `dashboard/src/api.ts` (the `status` field) and `dashboard/src/AgentList.tsx`.

## Naming plan

- **Who decides.** The maintainer decides the final nomenclature, later, and it is explained in this file.
- **Meanwhile.** New text (issues, pull requests, documents, comments) uses the terms above. If you need a word that is not here, add it here in the same pull request, with a status and a source. Do not use "node" in code or UI; keep it for the design discussion of #103 until it is decided.
- **When the names are decided**, one follow-up issue renames them everywhere. The list to cover: documents (README, `docs/`, this glossary), Go identifiers and package names (for example `fleet`, `BuiltinAgentID`), command-line subcommands and flags, the HTTP API paths and JSON fields (a breaking change: needs a version note), the Protobuf messages (a protocol change: needs the version rules), configuration keys, the dashboard and console strings **in English and Italian** (`dashboard/src`, `internal/console/strings.go`, and the translation mechanism of #125), the installer options, systemd unit names, and the titles and labels of issues.
- **What must not be renamed lightly:** anything a deployed hub or agent already uses on disk or on the wire (file names, table names, protocol field names, unit names), because an installed agent of the previous release must keep working (the N / N-1 rule).

## Italian UI labels

This is the only place in the documentation that has Italian. The GUI must support English and Italian (#120), but **it has no Italian text yet** (#125), so the right-hand column above is a **proposal** to keep future translations consistent. Where a term is a technical word that Italian users also say in English (hub, plugin, widget, dashboard), the label stays in English.
