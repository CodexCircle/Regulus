import type { auth } from "./auth.en";

export const authIt: Record<keyof typeof auth, string> = {
  "auth.loading": "Caricamento…",
  "auth.unreachable": "Impossibile raggiungere l'hub: {message}",
  "auth.tagline": "CodexCircle Server Manager",
  "auth.passwordsMismatch": "Le password non coincidono.",

  "auth.enroll.title": "Configura l'autenticazione a due fattori",
  "auth.enroll.intro": "Questo hub richiede l'autenticazione a due fattori. Potrai usare Regulus dopo averla attivata.",
  "auth.signOut": "Esci",

  "auth.login.title": "Accedi",
  "auth.login.username": "Nome utente",
  "auth.login.password": "Password",
  "auth.login.submit": "Accedi",
  "auth.login.forgot": "Hai dimenticato la password? Chiedi un link di reimpostazione a un amministratore.",

  "auth.code.title": "Autenticazione a due fattori",
  "auth.code.label": "Codice di autenticazione",
  "auth.code.hint": "Inserisci il codice a 6 cifre della tua app di autenticazione, oppure uno dei codici di recupero.",
  "auth.code.verify": "Verifica",
  "auth.code.back": "Indietro",

  "auth.recovery.one":
    "Hai effettuato l'accesso con un codice di recupero; ne resta {count}. Se non eri tu, cambia la password. Genera nuovi codici dalla pagina Account.",
  "auth.recovery.many":
    "Hai effettuato l'accesso con un codice di recupero; ne restano {count}. Se non eri tu, cambia la password. Genera nuovi codici dalla pagina Account.",
  "auth.recovery.dismiss": "Chiudi",

  "auth.setup.title": "Crea l'account amministratore",
  "auth.setup.intro":
    "Questo hub non ha ancora account. Copia il token di configurazione monouso dal log dell'hub (la riga che dice \"no accounts yet\").",
  "auth.setup.token": "Token di configurazione",
  "auth.setup.username": "Nome utente",
  "auth.setup.password": "Password (almeno 10 caratteri)",
  "auth.setup.repeat": "Ripeti la password",
  "auth.setup.submit": "Crea l'amministratore",

  "auth.reset.title": "Scegli una nuova password",
  "auth.reset.intro": "Il link funziona una sola volta e scade un'ora dopo che l'amministratore lo ha creato.",
  "auth.reset.password": "Nuova password (almeno 10 caratteri)",
  "auth.reset.repeat": "Ripeti la password",
  "auth.reset.submit": "Cambia password",
  "auth.reset.doneTitle": "Password cambiata",
  "auth.reset.done": "La password è stata cambiata e tutte le tue sessioni sono state chiuse.",
  "auth.reset.signIn": "Accedi",
};
