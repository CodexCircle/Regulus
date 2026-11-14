package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/protocol"
)

// openStream connects a raw agent stream with the credentials of an enrolled
// agent and completes the handshake.
func openStream(t *testing.T, h *Hub, hello *agentv1.Hello) (agentv1.AgentService_ConnectClient, string) {
	t.Helper()
	creds := enrollNew(t, h, "")
	conn, err := creds.Dial(h.AgentAddr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	stream, err := agentv1.NewAgentServiceClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hello.ProtocolVersion, hello.MinProtocolVersion = protocol.Version, protocol.MinSupported
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: hello}}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("no Welcome: %v", err)
	}
	return stream, creds.AgentID
}

func TestAgentMessageSizeIsLimited(t *testing.T) {
	h := startRemoteHub(t)
	stream, _ := openStream(t, h, &agentv1.Hello{AgentVersion: "9.9.9"})
	big := &agentv1.MetricsReport{Disks: []*agentv1.DiskMetrics{{MountPoint: strings.Repeat("x", 2*maxAgentMessageBytes)}}}
	_ = stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: big}})
	if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Errorf("a message of %d KiB: err = %v, want ResourceExhausted", 2*maxAgentMessageBytes>>10, err)
	}
}

func TestReportFloodIsDroppedAndBounded(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinAgent = false
	cfg.AgentListen = "127.0.0.1:0"
	h := startHubWithConfig(t, cfg, WithAgentIntervals(time.Hour, time.Hour))
	waitFor(t, "the agent listener", func() bool { return h.AgentAddr() != "" })
	stream, id := openStream(t, h, &agentv1.Hello{AgentVersion: "9.9.9", System: &agentv1.SystemInfo{Hostname: strings.Repeat("h", 4000)}})
	rep := &agentv1.MetricsReport{}
	for i := 0; i < 200; i++ {
		rep.Disks = append(rep.Disks, &agentv1.DiskMetrics{MountPoint: "/d", TotalBytes: 1})
	}
	for i := 0; i < 50; i++ {
		if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: rep}}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the first report", func() bool { return len(h.agents.metrics.history(id)) >= 1 })
	time.Sleep(300 * time.Millisecond) // let the rest of the flood arrive
	hist := h.agents.metrics.history(id)
	if len(hist) != 1 {
		t.Errorf("%d reports kept out of 50 sent at once, want 1", len(hist))
	}
	if n := len(hist[0].Report.Disks); n != maxListEntries {
		t.Errorf("a report with 200 disks was kept with %d, want %d", n, maxListEntries)
	}
	for _, a := range h.Agents() {
		if a.ID == id && len(a.System.GetHostname()) > maxTextBytes {
			t.Errorf("a host name of %d bytes was kept", len(a.System.GetHostname()))
		}
	}
}
