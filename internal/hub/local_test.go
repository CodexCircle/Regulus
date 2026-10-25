package hub

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/localstatus"
)

func TestLocalStatusSocket(t *testing.T) {
	h := startHubWithConfig(t, config.Default())
	waitFor(t, "built-in agent", func() bool {
		a := h.Agents()
		return len(a) == 1 && !a[0].LastHeartbeat.IsZero()
	})

	info, err := os.Stat(localstatus.SocketPath(h.cfg.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := localstatus.Fetch(ctx, h.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "9.9.9" || st.Addr == "" || !st.BuiltinAgent || st.StartedAt.IsZero() {
		t.Errorf("status = %+v", st)
	}
	if len(st.Agents) != 1 || st.Agents[0].ID != BuiltinAgentID || st.Agents[0].Status != statusConnected {
		t.Errorf("agents = %+v", st.Agents)
	}
	// No admin yet: the console shows the one-time token.
	if st.SetupToken == "" || st.SetupToken != h.auth.SetupToken() {
		t.Errorf("setup token = %q", st.SetupToken)
	}
}

func TestLocalStatusGoneWithTheHub(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := localstatus.Fetch(ctx, t.TempDir()); err == nil {
		t.Error("a hub answered where none runs")
	}
}
