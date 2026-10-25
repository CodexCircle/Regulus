package hub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/protocol"
)

// startHub runs a hub with short agent intervals and stops it with the test.
func startHub(t *testing.T) *Hub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, err := New(ctx, cfg, log, fstest.MapFS{}, WithAgentIntervals(100*time.Millisecond, 100*time.Millisecond))
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
		if err := <-done; err != nil {
			t.Errorf("Serve returned %v", err)
		}
		_ = h.Close()
	})
	return h
}

// connect opens an agent stream to the hub.
func connect(t *testing.T, h *Hub) agentv1.AgentService_ConnectClient {
	t.Helper()
	conn, err := h.AgentConn()
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
	return stream
}

func helloMsg(version uint32, id string) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		ProtocolVersion: version,
		AgentVersion:    "1.2.3",
		AgentId:         id,
		System:          &agentv1.SystemInfo{Hostname: "box", Os: "linux", Arch: "amd64", CpuCount: 4},
	}}}
}

// register connects, sends Hello and returns the stream and the Welcome.
func register(t *testing.T, h *Hub, id string) (agentv1.AgentService_ConnectClient, *agentv1.Welcome) {
	t.Helper()
	stream := connect(t, h)
	if err := stream.Send(helloMsg(protocol.Version, id)); err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if msg.GetWelcome() == nil {
		t.Fatalf("expected Welcome, got %v", msg)
	}
	return stream, msg.GetWelcome()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRegistrationAssignsIDAndRegistersAgent(t *testing.T) {
	h := startHub(t)
	stream, welcome := register(t, h, "")

	if welcome.GetAgentId() == "" {
		t.Fatal("hub did not assign an agent id")
	}
	if welcome.GetProtocolVersion() != protocol.Version {
		t.Fatalf("protocol version = %d, want %d", welcome.GetProtocolVersion(), protocol.Version)
	}
	if got := welcome.GetHeartbeatInterval().AsDuration(); got != 100*time.Millisecond {
		t.Fatalf("heartbeat interval = %v, want 100ms", got)
	}

	agents := h.Agents()
	if len(agents) != 1 {
		t.Fatalf("%d agents registered, want 1", len(agents))
	}
	a := agents[0]
	if a.ID != welcome.GetAgentId() || a.Version != "1.2.3" || a.System.GetHostname() != "box" || a.ConnectedAt.IsZero() {
		t.Fatalf("unexpected agent info: %+v", a)
	}

	// Closing the stream removes the agent.
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "agent to be removed", func() bool { return len(h.Agents()) == 0 })
}

func TestAgentKeepsProvidedID(t *testing.T) {
	h := startHub(t)
	_, welcome := register(t, h, "my-agent")
	if welcome.GetAgentId() != "my-agent" {
		t.Fatalf("agent id = %q, want my-agent", welcome.GetAgentId())
	}
	if got := h.Agents()[0].ID; got != "my-agent" {
		t.Fatalf("registered id = %q, want my-agent", got)
	}
}

func TestUnsupportedVersionIsRejected(t *testing.T) {
	h := startHub(t)
	for _, version := range []uint32{0, protocol.Version + 1} {
		stream := connect(t, h)
		if err := stream.Send(helloMsg(version, "")); err != nil {
			t.Fatal(err)
		}
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		reject := msg.GetReject()
		if reject == nil || reject.GetReason() != agentv1.Reject_REASON_UNSUPPORTED_PROTOCOL_VERSION ||
			reject.GetMinProtocolVersion() != protocol.MinSupported || reject.GetMaxProtocolVersion() != protocol.Version {
			t.Fatalf("version %d: unexpected answer %v", version, msg)
		}
		if _, err := stream.Recv(); err != io.EOF {
			t.Fatalf("version %d: stream should end after Reject, got %v", version, err)
		}
	}
	if n := len(h.Agents()); n != 0 {
		t.Fatalf("%d agents registered after rejections, want 0", n)
	}
}

func TestFirstMessageMustBeHello(t *testing.T) {
	h := startHub(t)
	stream := connect(t, h)
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	_, err := stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestHelloTimeout(t *testing.T) {
	h := startHub(t)
	h.agents.helloTimeout = 100 * time.Millisecond
	stream := connect(t, h)
	_, err := stream.Recv()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
}

func TestSecondHelloIsAProtocolError(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a")
	if err := stream.Send(helloMsg(protocol.Version, "a")); err != nil {
		t.Fatal(err)
	}
	_, err := stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestHeartbeatAndMetricsAreRecorded(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a")

	if !h.Agents()[0].LastHeartbeat.IsZero() || h.Agents()[0].LastMetrics != nil {
		t.Fatal("a new agent should have no heartbeat or metrics yet")
	}

	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{SentAt: timestamppb.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: &agentv1.MetricsReport{
		CollectedAt:   timestamppb.Now(),
		Cpu:           &agentv1.CpuMetrics{UsagePercent: 42},
		UptimeSeconds: 99,
	}}}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "heartbeat and metrics", func() bool {
		a := h.Agents()[0]
		return !a.LastHeartbeat.IsZero() && a.LastMetrics != nil
	})
	m := h.Agents()[0].LastMetrics
	if m.GetCpu().GetUsagePercent() != 42 || m.GetUptimeSeconds() != 99 {
		t.Fatalf("unexpected metrics: %v", m)
	}
}

func TestCommandRoundTrip(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a")

	// A minimal agent: answer every command successfully.
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			if cmd := msg.GetCommand(); cmd != nil {
				_ = stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_CommandResult{
					CommandResult: &agentv1.CommandResult{CommandId: cmd.GetId(), Success: cmd.GetPing() != nil},
				}})
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := h.SendCommand(ctx, "a", &agentv1.Command{Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.GetSuccess() {
		t.Fatalf("ping failed: %v", res)
	}

	// Commands are matched to their results by id, so two in a row work.
	res, err = h.SendCommand(ctx, "a", &agentv1.Command{Kind: &agentv1.Command_SetIntervals{SetIntervals: &agentv1.SetIntervals{}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetSuccess() {
		t.Fatal("the fake agent only accepts Ping, so SetIntervals should be reported as failed")
	}
}

func TestSendCommandErrors(t *testing.T) {
	h := startHub(t)
	ping := &agentv1.Command{Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}}

	if _, err := h.SendCommand(context.Background(), "nobody", ping); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("error = %v, want ErrAgentNotFound", err)
	}

	// An agent that never answers: the caller's context decides.
	register(t, h, "silent")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := h.SendCommand(ctx, "silent", ping); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
}

func TestDisconnectWhileWaitingForAResult(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a")

	// Close the stream as soon as the command arrives, without answering.
	go func() {
		if _, err := stream.Recv(); err == nil {
			_ = stream.CloseSend()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := h.SendCommand(ctx, "a", &agentv1.Command{Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}})
	if !errors.Is(err, ErrAgentDisconnected) {
		t.Fatalf("error = %v, want ErrAgentDisconnected", err)
	}
}

func TestReconnectReplacesStaleConnection(t *testing.T) {
	h := startHub(t)
	first, _ := register(t, h, "a")
	second, _ := register(t, h, "a")

	// The old stream is closed by the hub.
	_, err := first.Recv()
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("old stream error = %v, want AlreadyExists", err)
	}
	if n := len(h.Agents()); n != 1 {
		t.Fatalf("%d agents registered, want 1", n)
	}

	// The new stream still works and keeps the agent registered.
	if err := second.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "heartbeat on the new connection", func() bool { return !h.Agents()[0].LastHeartbeat.IsZero() })
}

func TestSilentAgentIsDropped(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a")

	// Intervals are 100ms and 3 are tolerated, so the stream ends well within 5s.
	_, err := stream.Recv()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	waitFor(t, "silent agent to be removed", func() bool { return len(h.Agents()) == 0 })
}
