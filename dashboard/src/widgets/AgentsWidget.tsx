import { AgentList } from "../AgentList";

// The servers overview. The session context tells it when the login has expired.
export function AgentsWidget() {
  return <AgentList heading={false} />;
}
