# Plugins (design draft)

> **Status: draft.** Nothing here is implemented yet. It gathers what has been decided in the plugin issues (#19, #20, #21, #22, #23, #63, #68) and the proposals still to be confirmed, so that the work in milestone M6 starts from one place. Points marked *proposed* may change.

## Core and plugins are versioned separately

The **core** (hub, agent, dashboard) has one version number and one release (`vX.Y.Z`). Each **plugin** has its own version and is released on its own, with tags of the form `plugin/<id>/vX.Y.Z` *(proposed)*. A plugin can therefore be updated without updating the core, and the other way round.

## Manifest (`plugin.yaml`) — #19, #68

Each plugin declares:

- `id` and `version`;
- `requires`: the range of core versions it works with;
- its components: agent side, hub side, dashboard widgets;
- the **permissions** it needs. They become permissions that can be given to roles like any other (#61), so `operator` and custom roles can use plugins;
- `provides`: integration points it offers to other plugins;
- `skills`: structured operations it offers to the AI assistant (name, parameter schema, permission, risk level, how to undo). See #68.

## Runtime — #19

A plugin runs as a **separate process** that talks gRPC with the host over a local socket. The host supervises and restarts it; a crashing plugin must not affect the agent. Because the boundary is gRPC, a plugin does not have to be written in Go.

## Index, installation and updates — #20

- CI publishes an `index.json` listing each plugin, its versions, **checksums** and **compatibility** (`requires`). The format has a `source` field so other sources can be added later (#23).
- The hub installs, updates and removes plugins from the dashboard, shows the requested permissions before installing, verifies signatures and distributes the plugin to the agents that need it.
- The same three update modes as the core (`manual`, `download`, `automatic`), with a per-plugin override.
- A plugin's version can be **pinned**, and the previous version is kept for a **rollback**.

## Dashboard widgets — #21

Plugins ship widgets as JavaScript modules served by the hub and registered at runtime. A broken widget only breaks its own frame.

## Repository layout — #22

Internal plugins live in `plugins/<id>/` in this repository *(proposed)*, with an example plugin and a guide for writing plugins.

## Third-party plugins — #23

Later, after the internal plugin system is stable: extra index sources, an **allowlist of trusted signers**, warnings for unsigned plugins and sandboxed permissions. Until then, formats are kept ready for it.

## Open questions

1. **Dependencies between plugins.** What happens when a plugin that others depend on (through `provides`) is updated or removed? Block, warn, or update the dependents together? #20 does not say yet.
2. **Tag format.** Is `plugin/<id>/vX.Y.Z` confirmed, and does the release workflow publish plugins from those tags?
3. **Plugin signatures.** Do internal plugins use the same Sigstore keyless signature as core releases?
4. **Skill parameters.** JSON Schema is the candidate (#68); also undecided: several risk levels per skill, and how the assistant discovers a server's skills.
