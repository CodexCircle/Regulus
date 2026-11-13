# Node types

The legend of the kinds of machine in a Regulus group. **Design draft:** only the **hub** and the **agent** (including the built-in agent) exist in the code today. Every other type below is *proposed*; nothing here is implemented (checked against the code on 2026-11-13). Names are provisional: the maintainer decides the final nomenclature later (see the [glossary](glossary.md), "Naming plan"). The design issue is [#160](https://github.com/CodexCircle/Regulus/issues/160); the role model it builds on is [#104](https://github.com/CodexCircle/Regulus/issues/104).

"Node" is used here for the design discussion only, as in #103: any machine in the group. Do not use it in code or UI yet.

## The rule that does not change

**Inside, every node connects the same way:** a `regulus` process on the machine opens an outbound, mutually authenticated gRPC connection to the active hub and is identified by the certificate the hub's CA issued to it ([architecture.md](architecture.md), sections 2 and 3). A node type does not add a new kind of connection to the hub. It adds **what the node is expected and allowed to do**, expressed as a role plus capabilities (proposal of #104: an enum plus capability flags, not unrelated booleans), so a new type does not require rewriting the others.

The exceptions are explicit and each has its own design issue: a node that **accepts inbound connections** (the access gateway) and **direct links between nodes** (later, see the end).

## The types

| Type | What it is | Talks to | Status |
|---|---|---|---|
| **Hub** (active hub) | Orchestrates the group: dashboard, API, database, CA, updates. Exactly one active per term. | every node (they connect to it) | **implemented** (`regulus hub`) |
| **Redundant hub** (informally "secondary hub"; further ones "tertiary"…) | Passive hub holding a copy of what the hub needs; takes over when the active hub is unreachable, or when forced. A **pure-hub** attribute means it hosts no workloads. | the active hub (replication); after failover, every node | designed in the issues of #103, **not implemented**. More than one redundant hub, with priority, is parked in [#113](https://github.com/CodexCircle/Regulus/issues/113). |
| **Compute node** (standard node) | A server that runs workloads and is managed from the hub. | the active hub only | **implemented** as the agent (`regulus agent`; the hub's own server is the built-in agent). Workload management is [#113](https://github.com/CodexCircle/Regulus/issues/113). |
| **Backup node** | A storage node that keeps the backups of the other nodes. Has a connection **to the hub only** and **cannot contact other nodes**. | the active hub only | proposed; no issue yet (this page records the idea). |
| **Access gateway** | A node with a public address that lets the Regulus Access phone app reach the hub from outside the local network. Forwards the phone's connection to the active hub; **mTLS stays end to end between phone and hub**. May later also host a notification server and, separately designed, an authentication server. | the active hub (outbound); phones (inbound, the only inbound listener of the group) | proposed in [#161](https://github.com/CodexCircle/Regulus/issues/161) (epic [#152](https://github.com/CodexCircle/Regulus/issues/152)). |
| **AI node** | A node that offers local AI (for example a server running Ollama) to other nodes. | the active hub, and possibly **directly** to the nodes that use it (see below) | idea only, not designed. |

A node has **one** type (the role); capabilities are switched on one by one by an administrator on the hub. An access gateway is a normal enrolled node plus the gateway capability, so it is rebuilt from nothing by enrolling again.

## Capabilities (proposed)

| Capability | Meaning | Used by |
|---|---|---|
| `relay` | forwards encrypted phone connections to the active hub; holds no secrets | access gateway |
| `notifications` | hosts the push path for phones; messages carry no secrets | access gateway (second step) |
| `authentication` | serves authentication for other programs; **holds keys on a public machine, so it needs its own threat model** | access gateway (later, separate design) |
| `backup-storage` | accepts backup data from the hub | backup node |
| `replica` | receives the replicated data of the hub | redundant hub |

The set is not final; it is what the issues mention today.

## Direct links between nodes (later)

Two nodes may need to work closely: an application on a compute node calling an AI node that runs Ollama is faster without going through the hub. This is **deliberately left for later** (parked with the other types in #113). The rule to keep: such a link is **authorised by the hub** (the hub says which node may call which, and issues the credentials), not discovered by the nodes, so that revoking a node at the hub also cuts its links.

## Where each type is designed

| Topic | Where |
|---|---|
| Roles, transitions, redundant hub, pure-hub | [hub-redundancy.md](hub-redundancy.md), #103 to #113 |
| Access gateway, phone access | [regulus-access.md](regulus-access.md), #152 and its children |
| Role model and capabilities | #104, #160 |
