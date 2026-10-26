import { AccountPage } from "./AccountPage";
import { Dashboard } from "./Dashboard";
import { ServerPage } from "./ServerPage";
import { ServersPage } from "./ServersPage";
import { useRoute, type Route } from "./route";
import { can } from "./api";
import type { Session } from "./session";
import { UpdatesPage } from "./UpdatesPage";
import { UsersPage } from "./UsersPage";

// A page with permissions is shown to anyone who holds one of them.
const nav: { route: Route; label: string; permissions?: string[] }[] = [
  { route: "/", label: "Dashboard" },
  { route: "/servers", label: "Servers" },
  { route: "/users", label: "Users", permissions: ["users.manage", "roles.manage", "audit.read"] },
  { route: "/updates", label: "Updates", permissions: ["core.update"] },
];

function Page({ route }: { route: Route }) {
  switch (route) {
    case "/servers":
      return <ServersPage />;
    case "/server":
      return <ServerPage />;
    case "/users":
      return <UsersPage />;
    case "/updates":
      return <UpdatesPage />;
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
          {nav.filter((n) => !n.permissions || n.permissions.some((p) => can(user, p))).map((n) => (
            <a key={n.route} href={`#${n.route}`} aria-current={route === n.route || (n.route === "/servers" && route === "/server") ? "page" : undefined}>
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
