import type { updates } from "./updates.en";

export const updatesIt: Record<keyof typeof updates, string> = {
  "updates.title": "Aggiornamenti",
  "updates.loading": "Caricamento…",
  "updates.unknown": "sconosciuta",

  "updates.channel.stable": "stabile",
  "updates.channel.beta": "beta",
  "updates.channel.alpha": "alpha",

  "updates.version.title": "Versione",
  "updates.version.runs": "Questo hub esegue",
  "updates.version.platform": "({platform}).",
  "updates.version.devBuild":
    "Questa è una build di sviluppo. Non si confronta con le release, quindi non le viene mai offerto un aggiornamento.",
  "updates.version.checksOffBefore":
    "I controlli degli aggiornamenti sono disattivati nella configurazione (",
  "updates.version.checksOffAfter":
    "), quindi l'hub non contatta nessun server.",
  "updates.version.availableLabel": "Aggiornamento disponibile",
  "updates.version.availableVersion": "Versione {version}",
  "updates.version.isAvailable": "è disponibile.",
  "updates.version.releasePage": "Pagina della release",
  "updates.version.upToDate":
    "Regulus è aggiornato: la release {channel} più recente è la {version}.",
  "updates.version.noRelease":
    "Il server delle release non elenca nessuna release {channel}.",
  "updates.version.checked": "Controllato {ago}.",
  "updates.version.notChecked": "Non ancora controllato.",
  "updates.version.checkFailed": "L'ultimo controllo non è riuscito: {error}",

  "updates.staged.label": "Aggiornamento scaricato",
  "updates.staged.version": "La versione",
  "updates.staged.downloaded": "è stata scaricata ({size}, {archive}).",
  "updates.staged.signature":
    "La sua firma è stata verificata: è stata creata dal workflow di release di questo progetto per questa versione e il file corrisponde.",
  "updates.staged.checksum":
    "Il suo SHA-256 corrisponde all'elenco dei checksum della release.",
  "updates.staged.itIs": "Risulta",
  "updates.staged.notInstalled": "non installata",
  "updates.staged.applyOn": ".",
  "updates.staged.applyOff":
    ": questo hub non è configurato per installare da solo gli aggiornamenti (updates.apply e l'unità regulus-update.path dell'installer).",
  "updates.staged.checksumNote":
    "L'elenco dei checksum arriva dallo stesso posto del file: individua un download danneggiato ma non dimostra chi lo ha pubblicato.",

  "updates.downloading": "Download in corso…",
  "updates.applying.head": "Installazione in corso: {what}…",
  "updates.applying.theUpdate": "aggiornamento",
  "updates.applying.rest":
    "L'hub si riavvia tra un momento e questa pagina può smettere di rispondere per un po'; ricontrolla da sola. Se la nuova versione non si avvia correttamente, viene rimessa la precedente.",
  "updates.stalled":
    "Nessuno ha preso in carico la richiesta da alcuni minuti. L'unità che installa gli aggiornamenti probabilmente non è in esecuzione:",
  "updates.last.applied": "Aggiornato alla {version} {ago}.",
  "updates.last.appliedFrom": "Aggiornato alla {version} dalla {from} {ago}.",
  "updates.last.rolledBack":
    "La versione {version} non si è avviata correttamente, quindi è stata rimessa la versione precedente.",
  "updates.last.notInstalled": "La versione {version} non è stata installata.",
  "updates.downloadFailed": "Il download non è riuscito: {error}",

  "updates.action.checkNow": "Controlla ora",
  "updates.action.checking": "Controllo in corso…",
  "updates.action.download": "Scarica {version}",
  "updates.action.install": "Installa {version}",
  "updates.action.installNow": "Installa ora",
  "updates.action.cancel": "Annulla",
  "updates.action.save": "Salva",
  "updates.action.stop": "Ferma",
  "updates.confirm.label": "Conferma installazione",
  "updates.confirm.text":
    "L'hub viene verificato e sostituito dalla versione {version} e si riavvia. Gli agent si riconnettono da soli. Se la nuova versione non si avvia, viene rimessa la precedente.",

  "updates.settings.title": "Impostazioni",
  "updates.settings.when": "Quando esce una nuova versione",
  "updates.mode.manual": "Avvisami soltanto",
  "updates.mode.download": "Scaricala e verificala, poi aspetta me",
  "updates.mode.automatic":
    "Scaricala e installala da sola, dentro una finestra di manutenzione",
  "updates.settings.applyOff":
    "L'installazione non è configurata su questo hub (updates.apply e l'unità regulus-update.path, che l'installer abilita).",
  "updates.settings.windowStarts": "Inizio della finestra",
  "updates.settings.windowEnds": "Fine della finestra",
  "updates.settings.windowNote":
    "Gli orari sono quelli del server dell'hub. L'hub si riavvia durante l'aggiornamento e una versione di cui è stato fatto il rollback non viene riprovata da sola.",
  "updates.settings.channel": "Canale",
  "updates.settings.channelStable": "Stabile",
  "updates.settings.channelBeta": "Beta (release e beta)",
  "updates.settings.channelAlpha": "Alpha (ogni release, alpha comprese)",

  "updates.rollout.title": "Server",
  "updates.rollout.intro":
    "I server gestiti da questo hub vengono aggiornati alla versione che esegue l'hub (",
  "updates.rollout.introAfter":
    "). La scaricano dall'hub, quindi non hanno bisogno di accesso a internet, e un server la cui nuova versione non si riconnette torna da solo alla precedente.",
  "updates.rollout.running":
    "Aggiornamento dei server, {size} alla volta. Si ferma al primo errore.",
  "updates.rollout.progress": "Avanzamento dell'aggiornamento",
  "updates.rollout.allCurrent":
    "Ogni server connesso esegue la {target}, oppure non è gestito da un agent che si può aggiornare.",
  "updates.rollout.behindOne": "1 server esegue una versione più vecchia.",
  "updates.rollout.behindMany":
    "{count} server eseguono una versione più vecchia.",
  "updates.rollout.behindLabel": "Server indietro",
  "updates.rollout.runs": "{name} esegue la {version}",
  "updates.rollout.atATime": "Alla volta",
  "updates.rollout.all": "Tutti",
  "updates.rollout.updateOne": "Aggiorna questo server alla {target}",
  "updates.rollout.updateMany": "Aggiorna {count} server alla {target}",
  "updates.rollout.lastDone": "Ultimo aggiornamento dei server: completato.",
  "updates.rollout.lastDoneAgo":
    "Ultimo aggiornamento dei server: completato, {ago}.",
  "updates.rollout.lastStopped": "Ultimo aggiornamento dei server: fermato.",
  "updates.rollout.lastStoppedAgo":
    "Ultimo aggiornamento dei server: fermato, {ago}.",
  "updates.rollout.lastFailed":
    "Ultimo aggiornamento dei server: fermato da un errore.",
  "updates.rollout.lastFailedAgo":
    "Ultimo aggiornamento dei server: fermato da un errore, {ago}.",
  "updates.rollout.lastLabel": "Ultimo aggiornamento",

  "updates.agent.pending": "in attesa",
  "updates.agent.offered": "sta scaricando l'aggiornamento",
  "updates.agent.installing": "in installazione, in attesa che torni",
  "updates.agent.updated": "aggiornato",
  "updates.agent.failed": "non riuscito",
  "updates.agent.skipped": "non provato",
  "updates.agent.row": "{name} ({from}): {status}",
  "updates.agent.rowMessage": "{name} ({from}): {status} — {message}",
};
