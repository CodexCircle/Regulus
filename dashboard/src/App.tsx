import { AuthFrame, AuthGate } from "./AuthGate";
import { LiveProvider } from "./LiveProvider";
import { ResetPage } from "./ResetPage";
import { useRoute } from "./route";
import { Shell } from "./Shell";

export function App() {
  // A reset link works without a login: the user has lost the password.
  if (useRoute() === "/reset") {
    return (
      <AuthFrame>
        <ResetPage />
      </AuthFrame>
    );
  }
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
