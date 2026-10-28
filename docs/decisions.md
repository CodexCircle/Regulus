# Design decisions

**English** · [Italiano](#italiano)

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

**Goal: install once.** After the first installation, an administrator should never have to install anything by hand from a terminal: hub, agents and (later) plugins are updated from the dashboard. Today this holds for hub and agents. Still missing: plugin updates (#19, #20) and two exceptions the README lists (an agent installed before the update feature, and a release that changes the systemd units or the configuration file).

**One piece at a time.** It must be possible to update an agent, the hub or a plugin on its own. Versions may require others: agents and hub negotiate the protocol and the hub accepts agents of its own release series and the previous one (#17, README "Versions"); plugins declare the core versions they need with `requires` (#19, [plugins](plugins.md)).

**Who decides.** The administrator chooses the mode in the dashboard: `manual` (tell me), `download` (download and verify, then wait for my approval) or `automatic` (install inside a maintenance window). The default for a new installation is **`download`**: nothing is installed without an administrator's approval.

**How the privileged step works.** The hub runs as the unprivileged user `regulus`. Installing a new binary is done by a root helper started by a systemd path unit (`regulus-update.path`, `regulus-agent-update.path`), which the installer enables unless `--no-self-update` is given. The helper acts only on a request naming a version, and checks everything again itself. Releases are signed with **Sigstore keyless** (GitHub Actions identity). An earlier design used a `sudoers.d` rule and minisign or cosign keys and disabled self-update for package installations; that design was replaced by the current one. Whether `sudoers.d` or a root helper is better for the other privileged actions (services, packages, Docker) is still open in #60.

## Verification status

No release has been published yet, and several parts have only been tested by unit and integration tests: the signed release, the `.deb` on a clean Ubuntu system, the agent installer on a second server, and installing and rolling back an update with real systemd. They will be checked together on a VM (#81). Until then, try the first release on a test server.

## Open questions

- Should the other privileged actions use a `sudoers.d` rule or a root helper like the updates? (#60)
- Plugin dependencies, signatures and layout: see the open questions in [plugins](plugins.md).

---

# Italiano

Le scelte alla base di Regulus che il codice da solo non spiega: cosa è stato deciso, perché e cosa resta aperto. La README dice *come* funzionano le cose; questo file dice *perché sono così*. Quando una decisione cambia, aggiorna questo file nella stessa pull request.

## Stack: Go e React

Hub e agent sono scritti in **Go**, la dashboard in **React con TypeScript**. Le alternative considerate erano: hub in C# con agent in Go, tutto in C#, Rust e Node.js.

Go è stato scelto perché:

- l'agent deve restare leggero anche su una VPS piccola (512 MB di RAM);
- produce **un solo eseguibile senza runtime** da installare, quindi un unico pacchetto serve entrambi i ruoli;
- l'agent apre una connessione **in uscita** verso l'hub, e il supporto gRPC di Go lo gestisce bene;
- i plugin sono **processi separati che parlano gRPC**, quindi non sono legati a Go: un plugin si può scrivere in C# o in qualsiasi linguaggio con gRPC.

I numeri su memoria e dimensioni citati durante la scelta erano ordini di grandezza, non misure.

## Licenza: Apache-2.0

Apache-2.0 è stata preferita ad AGPL-3.0. Una licenza permissiva rende più facile per altri scrivere e distribuire plugin (#23); AGPL proteggerebbe di più il progetto ma scoraggerebbe i plugin di terze parti. Cambiare licenza dopo contributi esterni è difficile, quindi la scelta è legata alla strategia sui plugin.

## Piattaforme

Ubuntu Server viene prima perché è il sistema usato per le prime prove reali. L'ordine è:

1. Ubuntu LTS 22.04, 24.04 e 26.04, amd64 e arm64, pacchetto `.deb` (finora su una macchina reale è stata provata 26.04 amd64, #81; se tenere 22.04 è aperto nella #89);
2. Debian, con lo stesso `.deb`;
3. distribuzioni RHEL-like, pacchetto `.rpm`.

Le altre distribuzioni con systemd possono usare l'archivio `tar.gz`. Un repository apt o un PPA per Ubuntu è rimandato.

## Roadmap: fasi e milestone

La README descrive quattro **fasi**; il lavoro è tracciato in sei **milestone**, da M1 a M6. I titoli delle issue riportano ancora la fase per cui erano state scritte (`[Phase 1]`, …), quindi titolo e milestone possono non coincidere: conta la milestone.

| Milestone | Contenuto | Fase |
|---|---|---|
| M1 | Fondamenta: scheletro, core dell'hub, contratto agent-hub, CI, workflow di release | 1 |
| M2 | Hub con agent integrato e dashboard: autenticazione, utenti, widget, pacchetto Ubuntu | 1 |
| M3 | Agent remoti: registrazione, dashboard multi-server, installer dell'agent | 2 |
| M4 | Hub e agent separati, versionamento del protocollo | 3 |
| M5 | Sistema di aggiornamento | 3 |
| M6 | Plugin | 4 |

Alcune issue sono state spostate dopo essere state scritte, quindi il titolo indica una fase precedente:

- **Autenticazione a due fattori (#49) in M3**: deve esserci prima che una dashboard gestisca più server.
- **Passkey (#50) in M4**: WebAuthn richiede HTTPS e un dominio pubblico (`public_url`), che arrivano con l'hub separato.
- **Gestione utenti (#48) e reset delle credenziali (#51) in M2**: senza reset, un amministratore bloccato non può rientrare.
- **Ruoli (#61) e console da terminale (#52) in M3**; **azioni privilegiate (#60) e inventario dei server (#62) in M4**; **AI Support (#63) in M6**.

I plugin di terze parti (#23) non hanno milestone: arrivano dopo che il sistema di plugin è stabile.

## Release e canali

Tre canali: **stable** (`v0.3.0`), **beta** (`v0.3.0-beta.N`; `-rc.N` conta come beta) e **alpha** (`v0.3.0-alpha.N`; qualsiasi altra pre-release conta come alpha, così un tipo sconosciuto non arriva mai a un canale più prudente). Ogni canale offre anche quelli più stabili: alpha vede tutto, beta vede beta e release, stable solo le release. Un hub parte sul canale della versione che esegue.

Le release si pubblicano dalla scheda Actions (**Publish release**), non scrivendo i tag a mano, così le versioni seguono sempre l'ordine SemVer: una versione passa dalle sue alpha, poi dalle beta, poi dalla release, e nessuna alpha arriva dopo una beta della stessa versione. Il workflow rifiuta un commit su cui la CI non è passata. Deve avviare il workflow di release **sul tag**, perché l'hub si fida solo di una firma fatta da `release.yml@refs/tags/<tag>`.

## Aggiornamenti

**Obiettivo: installare una volta.** Dopo la prima installazione, un amministratore non dovrebbe più installare nulla a mano da terminale: hub, agent e (in seguito) plugin si aggiornano dalla dashboard. Oggi vale per hub e agent. Mancano gli aggiornamenti dei plugin (#19, #20) e le due eccezioni elencate nella README (un agent installato prima della funzione di aggiornamento, e una release che cambia le unità systemd o il file di configurazione).

**Un pezzo alla volta.** Deve essere possibile aggiornare da solo un agent, l'hub o un plugin. Le versioni possono richiederne altre: agent e hub negoziano il protocollo e l'hub accetta agent della sua serie di release e della precedente (#17, README "Versions"); i plugin dichiarano le versioni del core di cui hanno bisogno con `requires` (#19, [plugin](plugins.md)).

**Chi decide.** L'amministratore sceglie la modalità nella dashboard: `manual` (avvisami), `download` (scarica e verifica, poi aspetta la mia approvazione) o `automatic` (installa dentro una finestra di manutenzione). Il default per una nuova installazione è **`download`**: nulla si installa senza l'approvazione di un amministratore.

**Come funziona il passo privilegiato.** L'hub gira con l'utente non privilegiato `regulus`. L'installazione del nuovo binario la fa un helper root avviato da una path unit di systemd (`regulus-update.path`, `regulus-agent-update.path`), che l'installer abilita salvo `--no-self-update`. L'helper agisce solo su una richiesta che indica una versione, e ricontrolla tutto da sé. Le release sono firmate con **Sigstore keyless** (identità di GitHub Actions). Un design precedente usava una regola `sudoers.d`, chiavi minisign o cosign e disattivava l'auto-aggiornamento nelle installazioni da pacchetto; è stato sostituito da quello attuale. Se per le altre azioni privilegiate (servizi, pacchetti, Docker) sia meglio `sudoers.d` o un helper root è ancora aperto nella #60.

## Stato delle verifiche

Non è ancora stata pubblicata nessuna release, e diverse parti sono state provate solo con test unitari e di integrazione: la release firmata, il `.deb` su un Ubuntu pulito, l'installer dell'agent su un secondo server, l'installazione e il rollback di un aggiornamento con systemd reale. Verranno verificate insieme su una VM (#81). Fino ad allora, prova la prima release su un server di prova.

## Questioni aperte

- Le altre azioni privilegiate devono usare una regola `sudoers.d` o un helper root come gli aggiornamenti? (#60)
- Dipendenze, firme e struttura dei plugin: vedi le questioni aperte in [plugin](plugins.md).
