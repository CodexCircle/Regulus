import type { core } from "./core.en";

export const coreIt: Record<keyof typeof core, string> = {
  "app.brand": "Regulus",
  "app.language": "Lingua",

  "nav.main": "Principale",
  "nav.dashboard": "Dashboard",
  "nav.servers": "Server",
  "nav.users": "Utenti",
  "nav.updates": "Aggiornamenti",
  "nav.signOut": "Esci",

  "time.day": "g",
  "time.hour": "h",
  "time.minute": "m",
  "time.second": "s",
  "time.ago": "{duration} fa",
  "time.never": "mai",

  "database.down":
    "Il database non risponde. Accesso e gran parte delle pagine non funzioneranno finché non torna disponibile. L'hub resta in funzione e si riconnette da solo.",
  "database.recovered": "Il database risponde di nuovo.",
};
