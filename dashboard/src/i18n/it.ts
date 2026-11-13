import type { MessageKey } from "./en";

// The Italian texts. Typed on the English keys, so a missing text does not
// compile. Technical words Italian users also say in English (hub, widget,
// dashboard) stay in English; see the glossary.
export const it: Record<MessageKey, string> = {
  "app.brand": "Regulus",
  "app.language": "Lingua",

  "nav.main": "Principale",
  "nav.dashboard": "Dashboard",
  "nav.servers": "Server",
  "nav.users": "Utenti",
  "nav.updates": "Aggiornamenti",
  "nav.signOut": "Esci",

  "database.down":
    "Il database non risponde. Accesso e gran parte delle pagine non funzioneranno finché non torna disponibile. L'hub resta in funzione e si riconnette da solo.",
  "database.recovered": "Il database risponde di nuovo.",
};
