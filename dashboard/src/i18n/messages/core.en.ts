// Header, navigation and the database banner.
export const core = {
  "app.brand": "Regulus",
  "app.language": "Language",

  "nav.main": "Main",
  "nav.dashboard": "Dashboard",
  "nav.servers": "Servers",
  "nav.users": "Users",
  "nav.updates": "Updates",
  "nav.signOut": "Sign out",

  "time.day": "d",
  "time.hour": "h",
  "time.minute": "m",
  "time.second": "s",
  "time.ago": "{duration} ago",
  "time.never": "never",

  "database.down":
    "The database is not answering. Signing in and most pages will fail until it is back. The hub keeps running and reconnects by itself.",
  "database.recovered": "The database is answering again.",
} as const;
