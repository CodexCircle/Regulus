import { AuthGate } from "./AuthGate";
import { LiveProvider } from "./LiveProvider";
import { Shell } from "./Shell";

export function App() {
  return (
    <AuthGate>
      {(session) => (
        <LiveProvider>
          <Shell {...session} />
        </LiveProvider>
      )}
    </AuthGate>
  );
}
