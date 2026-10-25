import { AgentList } from "./AgentList";
import { AuthGate } from "./AuthGate";

export function App() {
  return (
    <main>
      <h1>Regulus</h1>
      <p>CodexCircle Server Manager</p>
      <AuthGate>
        {({ user, signOut, expired }) => (
          <>
            <p>
              Signed in as <strong>{user.username}</strong> ({user.role}){" "}
              <button type="button" onClick={() => void signOut()}>
                Sign out
              </button>
            </p>
            <AgentList onUnauthorized={expired} />
          </>
        )}
      </AuthGate>
    </main>
  );
}
