import { useSyncExternalStore } from "react";

export type Route = "/" | "/servers" | "/server" | "/users" | "/account" | "/reset";

const known: readonly Route[] = ["/", "/servers", "/users", "/account"];

const RESET_PREFIX = "/reset/";
const SERVER_PREFIX = "/servers/";

// routeFromHash maps location.hash ("#/servers") to a page. Hash routes keep
// the hub simple: it serves one index.html and needs no fallback rules.
export function routeFromHash(hash: string): Route {
  const path = hash.replace(/^#/, "") || "/";
  if (path.startsWith(RESET_PREFIX) && path.length > RESET_PREFIX.length) return "/reset";
  if (path.startsWith(SERVER_PREFIX) && path.length > SERVER_PREFIX.length) return "/server";
  return known.find((r) => r === path) ?? "/";
}

// resetTokenFromHash returns the token of a password reset link
// ("#/reset/<token>"), or "" for any other address. The token is in the
// fragment, so it never reaches the hub's logs or other sites.
export function resetTokenFromHash(hash: string): string {
  const path = hash.replace(/^#/, "");
  return path.startsWith(RESET_PREFIX) ? decodeURIComponent(path.slice(RESET_PREFIX.length)) : "";
}

// serverIdFromHash returns the agent id of a server page ("#/servers/<id>"),
// or "" for any other address.
export function serverIdFromHash(hash: string): string {
  const path = hash.replace(/^#/, "");
  return path.startsWith(SERVER_PREFIX) ? decodeURIComponent(path.slice(SERVER_PREFIX.length)) : "";
}

// serverHref is the address of the page of one server.
export const serverHref = (id: string) => `#${SERVER_PREFIX}${encodeURIComponent(id)}`;

function subscribe(callback: () => void) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}

export function useRoute(): Route {
  return routeFromHash(useSyncExternalStore(subscribe, () => window.location.hash));
}
