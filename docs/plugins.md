# Plugins (design draft)

**English** · [Italiano](#italiano)

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

---

# Italiano

> **Stato: bozza.** Nulla di quanto segue è ancora implementato. Raccoglie quanto deciso nelle issue sui plugin (#19, #20, #21, #22, #23, #63, #68) e le proposte ancora da confermare, così il lavoro della milestone M6 parte da un unico posto. I punti segnati *proposta* possono cambiare.

## Core e plugin hanno versioni separate

Il **core** (hub, agent, dashboard) ha un solo numero di versione e una sola release (`vX.Y.Z`). Ogni **plugin** ha la sua versione e viene rilasciato per conto suo, con tag del tipo `plugin/<id>/vX.Y.Z` *(proposta)*. Un plugin si può quindi aggiornare senza aggiornare il core, e viceversa.

## Manifest (`plugin.yaml`) — #19, #68

Ogni plugin dichiara:

- `id` e `version`;
- `requires`: l'intervallo di versioni del core con cui funziona;
- i suoi componenti: lato agent, lato hub, widget della dashboard;
- i **permessi** di cui ha bisogno. Diventano permessi assegnabili ai ruoli come gli altri (#61), quindi `operator` e i ruoli personalizzati possono usare i plugin;
- `provides`: i punti di integrazione che offre ad altri plugin;
- `skills`: operazioni strutturate che offre all'assistente AI (nome, schema dei parametri, permesso, livello di rischio, come annullarle). Vedi #68.

## Runtime — #19

Un plugin gira come **processo separato** che parla gRPC con l'host tramite un socket locale. L'host lo sorveglia e lo riavvia; un plugin che si blocca non deve toccare l'agent. Poiché il confine è gRPC, un plugin non deve per forza essere scritto in Go.

## Indice, installazione e aggiornamenti — #20

- La CI pubblica un `index.json` con ogni plugin, le sue versioni, i **checksum** e la **compatibilità** (`requires`). Il formato ha un campo `source` per aggiungere altre fonti in futuro (#23).
- L'hub installa, aggiorna e rimuove i plugin dalla dashboard, mostra i permessi richiesti prima dell'installazione, verifica le firme e distribuisce il plugin agli agent che ne hanno bisogno.
- Le stesse tre modalità di aggiornamento del core (`manual`, `download`, `automatic`), con la possibilità di sceglierla per singolo plugin.
- La versione di un plugin si può **fissare**, e la versione precedente viene tenuta per il **rollback**.

## Widget della dashboard — #21

I plugin forniscono widget come moduli JavaScript serviti dall'hub e registrati a runtime. Un widget guasto rompe solo il proprio riquadro.

## Struttura nella repository — #22

I plugin interni stanno in `plugins/<id>/` in questa repository *(proposta)*, con un plugin di esempio e una guida per scriverne.

## Plugin di terze parti — #23

In un secondo momento, quando il sistema di plugin interni è stabile: fonti aggiuntive per l'indice, una **allowlist dei firmatari fidati**, avvisi per i plugin non firmati e permessi isolati. Fino ad allora i formati restano pronti per questo.

## Questioni aperte

1. **Dipendenze tra plugin.** Cosa succede quando si aggiorna o si rimuove un plugin da cui altri dipendono (tramite `provides`)? Bloccare, avvisare o aggiornare insieme i dipendenti? La #20 non lo dice ancora.
2. **Formato dei tag.** `plugin/<id>/vX.Y.Z` è confermato, e il workflow di release pubblica i plugin da quei tag?
3. **Firme dei plugin.** I plugin interni usano la stessa firma Sigstore keyless delle release del core?
4. **Parametri delle skill.** JSON Schema è il candidato (#68); da decidere anche più livelli di rischio per skill e come l'assistente scopre le skill di un server.
