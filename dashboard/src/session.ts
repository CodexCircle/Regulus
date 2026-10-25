import { createContext, useContext } from "react";
import { can, type User } from "./api";

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

// useCan tells whether the signed-in user holds a permission.
export function useCan(permission: string): boolean {
  return can(useSession()?.user, permission);
}
