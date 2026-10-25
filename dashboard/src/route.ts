import { useSyncExternalStore } from "react";

export type Route = "/" | "/servers" | "/users" | "/account";

const known: readonly Route[] = ["/", "/servers", "/users", "/account"];

// routeFromHash maps location.hash ("#/servers") to a page. Hash routes keep
// the hub simple: it serves one index.html and needs no fallback rules.
export function routeFromHash(hash: string): Route {
  const path = hash.replace(/^#/, "") || "/";
  return known.find((r) => r === path) ?? "/";
}

function subscribe(callback: () => void) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}

export function useRoute(): Route {
  return routeFromHash(useSyncExternalStore(subscribe, () => window.location.hash));
}
