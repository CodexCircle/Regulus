import type { MessageKey } from "./en";
import { coreIt } from "./messages/core.it";
import { authIt } from "./messages/auth.it";
import { serversIt } from "./messages/servers.it";
import { updatesIt } from "./messages/updates.it";
import { usersIt } from "./messages/users.it";
import { accountIt } from "./messages/account.it";

// The Italian texts, typed on the English keys, so a missing text does not
// compile. Technical words Italian users also say in English (hub, widget,
// dashboard) stay in English; see the glossary.
export const itAreas = { coreIt, authIt, serversIt, updatesIt, usersIt, accountIt } as const;

export const it: Record<MessageKey, string> = { ...coreIt, ...authIt, ...serversIt, ...updatesIt, ...usersIt, ...accountIt };
