package protocol_test

import (
	"context"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/inproc"
	"github.com/CodexCircle/Regulus/internal/protocol"
)

// fakeHub implements the hub side of the contract and records what it saw.
type fakeHub struct {
	agentv1.UnimplementedAgentServiceServer
	seenHello   chan *agentv1.Hello
	seenMetrics chan *agentv1.MetricsReport
	seenResult  chan *agentv1.CommandResult
}

func (h *fakeHub) Connect(stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return io.ErrUnexpectedEOF
	}
	h.seenHello <- hello

	if hello.GetProtocolVersion() != protocol.Version {
		return stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Reject{Reject: &agentv1.Reject{
			Reason:             agentv1.Reject_REASON_UNSUPPORTED_PROTOCOL_VERSION,
			Message:            "unsupported protocol version",
			MinProtocolVersion: protocol.Version,
			MaxProtocolVersion: protocol.Version,
		}}})
	}

	if err := stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Welcome{Welcome: &agentv1.Welcome{
		ProtocolVersion:   protocol.Version,
		HubVersion:        "test",
		AgentId:           "agent-1",
		HeartbeatInterval: durationpb.New(5 * time.Second),
		MetricsInterval:   durationpb.New(10 * time.Second),
	}}}); err != nil {
		return err
	}
	if err := stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Command{Command: &agentv1.Command{
		Id:   "cmd-1",
		Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}},
	}}}); err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch p := msg.GetPayload().(type) {
		case *agentv1.AgentMessage_Metrics:
			h.seenMetrics <- p.Metrics
		case *agentv1.AgentMessage_CommandResult:
			h.seenResult <- p.CommandResult
		}
	}
}

func newHub(t *testing.T) (*fakeHub, agentv1.AgentServiceClient) {
	t.Helper()
	hub := &fakeHub{
		seenHello:   make(chan *agentv1.Hello, 1),
		seenMetrics: make(chan *agentv1.MetricsReport, 1),
		seenResult:  make(chan *agentv1.CommandResult, 1),
	}
	tr := inproc.New()
	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, hub)
	go func() { _ = srv.Serve(tr.Listener()) }() // returns when the test stops the server
	t.Cleanup(srv.Stop)

	conn, err := tr.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return hub, agentv1.NewAgentServiceClient(conn)
}

func hello(version uint32) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		ProtocolVersion: version,
		AgentVersion:    "test",
		System:          &agentv1.SystemInfo{Hostname: "box", Os: "linux", Arch: "amd64", CpuCount: 4},
	}}}
}

func TestFullExchangeOverInProcessTransport(t *testing.T) {
	hub, client := newHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(hello(protocol.Version)); err != nil {
		t.Fatal(err)
	}

	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	welcome := msg.GetWelcome()
	if welcome == nil || welcome.GetProtocolVersion() != protocol.Version || welcome.GetAgentId() != "agent-1" {
		t.Fatalf("unexpected first hub message: %v", msg)
	}
	if got := welcome.GetHeartbeatInterval().AsDuration(); got != 5*time.Second {
		t.Fatalf("heartbeat interval = %v, want 5s", got)
	}
	if got := (<-hub.seenHello).GetSystem().GetHostname(); got != "box" {
		t.Fatalf("hub saw hostname %q, want box", got)
	}

	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{
		Heartbeat: &agentv1.Heartbeat{SentAt: timestamppb.Now()},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: &agentv1.MetricsReport{
		CollectedAt:   timestamppb.Now(),
		Cpu:           &agentv1.CpuMetrics{UsagePercent: 12.5},
		Memory:        &agentv1.MemoryMetrics{TotalBytes: 1 << 30, UsedBytes: 1 << 29},
		Disks:         []*agentv1.DiskMetrics{{MountPoint: "/", Filesystem: "ext4", TotalBytes: 100, UsedBytes: 40}},
		UptimeSeconds: 42,
	}}}); err != nil {
		t.Fatal(err)
	}
	if m := <-hub.seenMetrics; m.GetCpu().GetUsagePercent() != 12.5 || m.GetDisks()[0].GetMountPoint() != "/" {
		t.Fatalf("hub received unexpected metrics: %v", m)
	}

	msg, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	cmd := msg.GetCommand()
	if cmd == nil || cmd.GetPing() == nil {
		t.Fatalf("expected a Ping command, got %v", msg)
	}
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_CommandResult{
		CommandResult: &agentv1.CommandResult{CommandId: cmd.GetId(), Success: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if r := <-hub.seenResult; r.GetCommandId() != "cmd-1" || !r.GetSuccess() {
		t.Fatalf("hub received unexpected command result: %v", r)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedVersionIsRejected(t *testing.T) {
	_, client := newHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(hello(protocol.Version + 100)); err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	reject := msg.GetReject()
	if reject == nil || reject.GetReason() != agentv1.Reject_REASON_UNSUPPORTED_PROTOCOL_VERSION {
		t.Fatalf("expected a version rejection, got %v", msg)
	}
}
