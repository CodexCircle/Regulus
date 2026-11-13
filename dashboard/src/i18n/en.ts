// The English texts of the dashboard: the source of truth. A key is a stable
// dotted name, never the English text. {name} is a placeholder. Every key
// needs an Italian text in it.ts; the compiler and a test check that.
export const en = {
  "app.brand": "Regulus",
  "app.language": "Language",

  "nav.main": "Main",
  "nav.dashboard": "Dashboard",
  "nav.servers": "Servers",
  "nav.users": "Users",
  "nav.updates": "Updates",
  "nav.signOut": "Sign out",

  "database.down":
    "The database is not answering. Signing in and most pages will fail until it is back. The hub keeps running and reconnects by itself.",
  "database.recovered": "The database is answering again.",
} as const;

export type MessageKey = keyof typeof en;
