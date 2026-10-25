import { AuthGate } from "./AuthGate";
import { Shell } from "./Shell";

export function App() {
  return (
    <AuthGate>
      {(session) => <Shell {...session} />}
    </AuthGate>
  );
}
