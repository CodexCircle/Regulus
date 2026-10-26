package hub

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"testing/fstest"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

func startHubWithConfig(t *testing.T, cfg config.Config) *Hub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cfg.DataDir = t.TempDir()
	cfg.Updates.Check = false // tests never reach for the network
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, err := New(ctx, cfg, log, fstest.MapFS{},
		WithVersion("9.9.9"), WithAgentIntervals(50*time.Millisecond, 50*time.Millisecond))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve returned %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return after cancellation")
		}
		_ = h.Close()
	})
	return h
}

func TestBuiltinAgentConnectsThroughTheContract(t *testing.T) {
	h := startHubWithConfig(t, config.Default())

	waitFor(t, "built-in agent heartbeat and metrics", func() bool {
		agents := h.Agents()
		return len(agents) == 1 && !agents[0].LastHeartbeat.IsZero() && agents[0].LastMetrics != nil
	})
	a := h.Agents()[0]
	if a.ID != BuiltinAgentID || a.Version != "9.9.9" {
		t.Fatalf("unexpected agent: id = %q, version = %q", a.ID, a.Version)
	}
	if a.System.GetOs() == "" || a.System.GetArch() == "" || a.System.GetCpuCount() == 0 {
		t.Fatalf("system info is incomplete: %v", a.System)
	}
	if a.LastMetrics.GetCollectedAt() == nil {
		t.Fatalf("metrics have no timestamp: %v", a.LastMetrics)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := h.SendCommand(ctx, BuiltinAgentID, &agentv1.Command{Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}})
	if err != nil || !res.GetSuccess() {
		t.Fatalf("ping the built-in agent: result = %v, err = %v", res, err)
	}
}

func TestBuiltinAgentCanBeDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinAgent = false
	h := startHubWithConfig(t, cfg)

	time.Sleep(300 * time.Millisecond) // several intervals: an agent would have shown up
	if n := len(h.Agents()); n != 0 {
		t.Fatalf("%d agents connected, want none when builtin_agent is false", n)
	}
}
