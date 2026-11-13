// The English texts of the dashboard: the source of truth. A key is a stable
// dotted name that starts with the area ("nav.servers", "auth.signIn"), never
// the English text. {name} is a placeholder. The texts live in one file per
// area under messages/, so pull requests that migrate different pages do not
// touch the same file. Every key needs an Italian text in the matching
// messages/<area>.it.ts; the compiler and a test check that.
import { core } from "./messages/core.en";
import { auth } from "./messages/auth.en";
import { servers } from "./messages/servers.en";
import { updates } from "./messages/updates.en";
import { users } from "./messages/users.en";
import { account } from "./messages/account.en";

export const areas = { core, auth, servers, updates, users, account } as const;

export const en = { ...core, ...auth, ...servers, ...updates, ...users, ...account } as const;

export type MessageKey = keyof typeof en;
