package hub

import (
	"context"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/auth"
)

// runAgentVersion is runAgent for an agent that reports another release.
func runAgentVersion(t *testing.T, h *Hub, creds *agent.Credentials, version string) (stop func()) {
	t.Helper()
	conn, err := creds.Dial(h.AgentAddr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = agent.Run(ctx, conn, agent.Options{
			Version: version, ID: creds.AgentID, Log: quiet(),
			MinBackoff: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		})
		_ = conn.Close()
	}()
	stop = func() { cancel(); <-done }
	t.Cleanup(stop)
	return stop
}

// The hub (9.9.9 in these tests) takes agents of its own series and the
// previous one, tells the dashboard which are behind, and refuses the rest
// with a reason that stays visible until they are updated.
func TestAgentsOfOtherReleasesAreSortedByCompatibility(t *testing.T) {
	h := startRemoteHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)

	current := enrollNew(t, h, "current")
	behind := enrollNew(t, h, "behind")
	ahead := enrollNew(t, h, "ahead")
	tooOld := enrollNew(t, h, "old")
	runAgentVersion(t, h, current, "9.9.9")
	runAgentVersion(t, h, behind, "9.8.1")
	runAgentVersion(t, h, ahead, "9.10.0")
	stopOld := runAgentVersion(t, h, tooOld, "9.7.0")

	waitFor(t, "three agents to connect and one to be refused", func() bool {
		if len(h.Agents()) != 3 {
			return false
		}
		_, refused := h.Rejection(tooOld.AgentID)
		return refused
	})

	byName := map[string]agentJSON{}
	for _, a := range admin.agents("?all=true") {
		byName[a.Name] = a
	}
	for name, want := range map[string]string{"current": "current", "behind": "behind", "ahead": "ahead"} {
		a := byName[name]
		if a.Status != statusConnected || a.Compatibility != want || a.ProtocolVersion == 0 {
			t.Errorf("%s: %+v, want connected and %s", name, a, want)
		}
	}
	old := byName["old"]
	if old.Status != statusIncompatible || old.Version != "9.7.0" || old.IncompatibleReason == "" || old.ConnectedAt != nil {
		t.Errorf("too old: %+v", old)
	}

	// The refusal is dropped once the agent comes back with a supported release
	// (the old process is gone by then, as it is after a real update).
	stopOld()
	runAgentVersion(t, h, tooOld, "9.9.0")
	waitFor(t, "the updated agent to connect", func() bool { return len(h.Agents()) == 4 })
	if _, refused := h.Rejection(tooOld.AgentID); refused {
		t.Error("the refusal is still remembered after the agent connected")
	}
}
