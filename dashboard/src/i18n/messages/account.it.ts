import type { account } from "./account.en";

// Italian draft, to be read by the maintainer.
export const accountIt: Record<keyof typeof account, string> = {
  "account.title": "Account",
  "account.loading": "Caricamento…",

  "account.password.title": "Cambia password",
  "account.password.current": "Password attuale",
  "account.password.new": "Nuova password",
  "account.password.changed": "Password cambiata. Le tue altre sessioni sono state disconnesse.",
  "account.password.submit": "Cambia password",

  "account.sessions.title": "Le tue sessioni",
  "account.sessions.loadError": "Impossibile caricare le sessioni: {message}",
  "account.sessions.browser": "Browser",
  "account.sessions.from": "Da",
  "account.sessions.lastUsed": "Ultimo utilizzo",
  "account.sessions.actions": "Azioni",
  "account.sessions.unknown": "sconosciuto",
  "account.sessions.thisOne": "adesso (questa sessione)",
  "account.sessions.signOutFrom": "Disconnetti {from}",
  "account.sessions.signOutSession": "Disconnetti la sessione",

  "account.twoFactor.title": "Autenticazione a due fattori",
  "account.twoFactor.loadError": "Impossibile caricare lo stato: {message}",
  "account.twoFactor.yourPassword": "La tua password",
  "account.twoFactor.mandatoryNotice": "Questo hub richiede l'autenticazione a due fattori. Configurala per continuare.",
  "account.twoFactor.intro":
    "Aggiungi un secondo passaggio all'accesso: un codice di un'app di autenticazione sul telefono, oltre alla password.",
  "account.twoFactor.setUp": "Configura l'autenticazione a due fattori",
  "account.twoFactor.scan":
    "1. Inquadra questo codice con un'app di autenticazione (Google Authenticator, Aegis, 1Password, Bitwarden…), oppure digita la chiave nell'app.",
  "account.twoFactor.qrAlt": "Codice QR per aggiungere questo account a un'app di autenticazione",
  "account.twoFactor.key": "Chiave:",
  "account.twoFactor.secretKey": "Chiave segreta",
  "account.twoFactor.enterCode": "2. Inserisci il codice di 6 cifre mostrato dall'app",
  "account.twoFactor.turnOn": "Attiva l'autenticazione a due fattori",
  "account.twoFactor.isOn": "L'autenticazione a due fattori è attiva. Serve un codice dell'app a ogni accesso.",
  "account.twoFactor.codesLeft": "Codici di recupero rimasti:",
  "account.twoFactor.runningOut": "I codici di recupero stanno per finire. Creane di nuovi e conservali in un posto sicuro.",
  "account.twoFactor.makeNew": "Crea nuovi codici di recupero",
  "account.twoFactor.cannotTurnOff": "L'hub richiede l'autenticazione a due fattori, quindi non si può disattivare.",
  "account.twoFactor.turnOff": "Disattiva l'autenticazione a due fattori",
  "account.twoFactor.renewWarning": "I vecchi codici di recupero smettono di funzionare.",
  "account.twoFactor.disableWarning": "Il tuo account sarà protetto solo dalla password.",
  "account.twoFactor.confirmHint": "Conferma con la tua password e un codice.",
  "account.twoFactor.codeOrRecovery": "Codice dell'app, oppure un codice di recupero",
  "account.twoFactor.turnOffShort": "Disattiva",
  "account.twoFactor.cancel": "Annulla",

  "account.recovery.label": "Codici di recupero",
  "account.recovery.saveNow": "Salva subito questi codici di recupero.",
  "account.recovery.explain":
    "Ognuno vale una sola volta, al posto del codice dell'app, se perdi il telefono. Non verranno mostrati di nuovo.",
  "account.recovery.copied": "Copiato",
  "account.recovery.copy": "Copia i codici",
  "account.recovery.download": "Scarica i codici",
  "account.recovery.saved": "Ho salvato questi codici in un posto sicuro",
  "account.recovery.done": "Fatto",
  "account.recovery.fileHeader": "Codici di recupero di Regulus",
};
