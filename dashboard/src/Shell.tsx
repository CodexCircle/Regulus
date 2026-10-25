import { AgentList } from "./AgentList";
import { Dashboard } from "./Dashboard";
import { useRoute, type Route } from "./route";
import type { Session } from "./session";

const nav: { route: Route; label: string }[] = [
  { route: "/", label: "Dashboard" },
  { route: "/servers", label: "Servers" },
];

// Shell is the frame of the signed-in dashboard: header, navigation and the
// current page.
export function Shell({ user, signOut }: Session) {
  const route = useRoute();
  return (
    <div className="shell">
      <header className="shell-header">
        <a className="brand" href="#/">
          Regulus
        </a>
        <nav aria-label="Main">
          {nav.map((n) => (
            <a key={n.route} href={`#${n.route}`} aria-current={route === n.route ? "page" : undefined}>
              {n.label}
            </a>
          ))}
        </nav>
        <div className="account">
          <span>
            <strong>{user.username}</strong> ({user.role})
          </span>
          <button type="button" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      </header>
      <main>{route === "/servers" ? <AgentList /> : <Dashboard />}</main>
    </div>
  );
}
