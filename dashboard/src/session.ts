import { createContext, useContext } from "react";
import type { User } from "./api";

export interface Session {
  user: User;
  // signOut ends the session on the hub; expired is called when the hub says
  // the session is gone (HTTP 401) and only returns to the login page.
  signOut: () => Promise<void>;
  expired: () => void;
}

export const SessionContext = createContext<Session | null>(null);

// useSession returns the current session, or null outside a signed-in page.
export function useSession(): Session | null {
  return useContext(SessionContext);
}
