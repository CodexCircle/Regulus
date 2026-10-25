import { useSyncExternalStore } from "react";

export type Route = "/" | "/servers" | "/users" | "/account" | "/reset";

const known: readonly Route[] = ["/", "/servers", "/users", "/account"];

const RESET_PREFIX = "/reset/";

// routeFromHash maps location.hash ("#/servers") to a page. Hash routes keep
// the hub simple: it serves one index.html and needs no fallback rules.
export function routeFromHash(hash: string): Route {
  const path = hash.replace(/^#/, "") || "/";
  if (path.startsWith(RESET_PREFIX) && path.length > RESET_PREFIX.length) return "/reset";
  return known.find((r) => r === path) ?? "/";
}

// resetTokenFromHash returns the token of a password reset link
// ("#/reset/<token>"), or "" for any other address. The token is in the
// fragment, so it never reaches the hub's logs or other sites.
export function resetTokenFromHash(hash: string): string {
  const path = hash.replace(/^#/, "");
  return path.startsWith(RESET_PREFIX) ? decodeURIComponent(path.slice(RESET_PREFIX.length)) : "";
}

function subscribe(callback: () => void) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}

export function useRoute(): Route {
  return routeFromHash(useSyncExternalStore(subscribe, () => window.location.hash));
}
