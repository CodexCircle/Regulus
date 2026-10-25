import { AccountPage } from "./AccountPage";
import { AgentList } from "./AgentList";
import { Dashboard } from "./Dashboard";
import { useRoute, type Route } from "./route";
import type { Session } from "./session";
import { UsersPage } from "./UsersPage";

const nav: { route: Route; label: string; adminOnly?: boolean }[] = [
  { route: "/", label: "Dashboard" },
  { route: "/servers", label: "Servers" },
  { route: "/users", label: "Users", adminOnly: true },
];

function Page({ route }: { route: Route }) {
  switch (route) {
    case "/servers":
      return <AgentList />;
    case "/users":
      return <UsersPage />;
    case "/account":
      return <AccountPage />;
    default:
      return <Dashboard />;
  }
}

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
          {nav.filter((n) => !n.adminOnly || user.role === "admin").map((n) => (
            <a key={n.route} href={`#${n.route}`} aria-current={route === n.route ? "page" : undefined}>
              {n.label}
            </a>
          ))}
        </nav>
        <div className="account">
          <a href="#/account" aria-current={route === "/account" ? "page" : undefined}>
            <strong>{user.username}</strong> ({user.role})
          </a>
          <button type="button" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      </header>
      <main>
        <Page route={route} />
      </main>
    </div>
  );
}
