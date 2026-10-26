package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/inproc"
	"github.com/CodexCircle/Regulus/internal/protocol"
)

// fakeHub is a hub whose behavior each test scripts per connection.
type fakeHub struct {
	agentv1.UnimplementedAgentServiceServer

	mu          sync.Mutex
	connections int
	handler     func(n int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error
}

func (h *fakeHub) Connect(stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
	h.mu.Lock()
	h.connections++
	n := h.connections
	h.mu.Unlock()
	return h.handler(n, stream)
}

// newFakeHub serves handler and returns a client connection to it.
func newFakeHub(t *testing.T, handler func(n int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error) *grpc.ClientConn {
	t.Helper()
	tr := inproc.New()
	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, &fakeHub{handler: handler})
	go func() { _ = srv.Serve(tr.Listener()) }()
	t.Cleanup(srv.Stop)

	conn, err := tr.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func welcome(id string, heartbeat, metrics time.Duration) *agentv1.HubMessage {
	return &agentv1.HubMessage{Payload: &agentv1.HubMessage_Welcome{Welcome: &agentv1.Welcome{
		ProtocolVersion:   protocol.Version,
		AgentId:           id,
		HeartbeatInterval: durationpb.New(heartbeat),
		MetricsInterval:   durationpb.New(metrics),
	}}}
}

func opts() Options {
	return Options{
		Version:    "test",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
	}
}

// runAgent runs the agent in the background and returns its result channel.
func runAgent(t *testing.T, conn grpc.ClientConnInterface, o Options) (cancel func(), result <-chan error) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- Run(ctx, conn, o) }()
	t.Cleanup(stop)
	return stop, res
}

func TestHelloCarriesVersionAndSystemInfo(t *testing.T) {
	got := make(chan *agentv1.Hello, 1)
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		got <- msg.GetHello()
		if err := stream.Send(welcome("assigned", time.Hour, time.Hour)); err != nil {
			return err
		}
		<-stream.Context().Done()
		return nil
	})
	runAgent(t, conn, opts())

	select {
	case hello := <-got:
		if hello.GetProtocolVersion() != protocol.Version || hello.GetAgentVersion() != "test" || hello.GetAgentId() != "" {
			t.Fatalf("unexpected Hello: %v", hello)
		}
		sys := hello.GetSystem()
		if sys.GetOs() == "" || sys.GetArch() == "" || sys.GetCpuCount() == 0 {
			t.Fatalf("system info is incomplete: %v", sys)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no Hello received")
	}
}

func TestSendsHeartbeatAndMetricsRightAfterWelcome(t *testing.T) {
	var kinds = make(chan string, 8)
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		if _, err := stream.Recv(); err != nil { // Hello
			return err
		}
		// Hour-long intervals: only the immediate first reports can arrive.
		if err := stream.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		for {
			msg, err := stream.Recv()
			if err != nil {
				return nil
			}
			switch msg.GetPayload().(type) {
			case *agentv1.AgentMessage_Heartbeat:
				kinds <- "heartbeat"
			case *agentv1.AgentMessage_Metrics:
				kinds <- "metrics"
			}
		}
	})
	runAgent(t, conn, opts())

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case k := <-kinds:
			seen[k] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("only saw %v, want a heartbeat and a metrics report", seen)
		}
	}
}

type failingCollector struct{}

func (failingCollector) Collect(context.Context) (*agentv1.MetricsReport, error) {
	return nil, errors.New("disk on fire")
}

func TestFailingCollectorDoesNotBreakTheConnection(t *testing.T) {
	heartbeats := make(chan struct{}, 8)
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		if err := stream.Send(welcome("a", 20*time.Millisecond, 20*time.Millisecond)); err != nil {
			return err
		}
		for {
			msg, err := stream.Recv()
			if err != nil {
				return nil
			}
			if msg.GetHeartbeat() != nil {
				heartbeats <- struct{}{}
			}
		}
	})
	o := opts()
	o.Collector = failingCollector{}
	runAgent(t, conn, o)

	for range 3 { // several heartbeats in a row on the same connection
		select {
		case <-heartbeats:
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeats stopped after the collector failed")
		}
	}
}

func TestCommands(t *testing.T) {
	results := make(chan *agentv1.CommandResult, 8)
	heartbeats := make(chan struct{}, 64)
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		// Hour-long intervals, then SetIntervals speeds heartbeats up.
		if err := stream.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		for _, cmd := range []*agentv1.Command{
			{Id: "1", Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}},
			{Id: "2", Kind: &agentv1.Command_SetIntervals{SetIntervals: &agentv1.SetIntervals{HeartbeatInterval: durationpb.New(-time.Second)}}},
			{Id: "3", Kind: &agentv1.Command_SetIntervals{SetIntervals: &agentv1.SetIntervals{HeartbeatInterval: durationpb.New(20 * time.Millisecond)}}},
			{Id: "4"}, // no kind: a command this agent does not know
		} {
			if err := stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Command{Command: cmd}}); err != nil {
				return err
			}
		}
		for {
			msg, err := stream.Recv()
			if err != nil {
				return nil
			}
			switch p := msg.GetPayload().(type) {
			case *agentv1.AgentMessage_CommandResult:
				results <- p.CommandResult
			case *agentv1.AgentMessage_Heartbeat:
				heartbeats <- struct{}{}
			}
		}
	})
	runAgent(t, conn, opts())

	want := map[string]bool{"1": true, "2": false, "3": true, "4": false}
	for range want {
		select {
		case res := <-results:
			ok, known := want[res.GetCommandId()]
			if !known || res.GetSuccess() != ok {
				t.Fatalf("unexpected result: %v (want success=%v)", res, ok)
			}
			if !ok && res.GetError() == "" {
				t.Fatalf("a failed command must say why: %v", res)
			}
			delete(want, res.GetCommandId())
		case <-time.After(5 * time.Second):
			t.Fatalf("missing results for commands %v", want)
		}
	}

	// Command 3 sped heartbeats up from one per hour to one per 20ms.
	for range 4 {
		select {
		case <-heartbeats:
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeats did not speed up after SetIntervals")
		}
	}
}

func TestRevocationIsFatal(t *testing.T) {
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		return stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Reject{Reject: &agentv1.Reject{
			Reason:  agentv1.Reject_REASON_UNAUTHORIZED,
			Message: "revoked",
		}}})
	})
	_, res := runAgent(t, conn, opts())

	select {
	case err := <-res:
		var rejected *RejectedError
		if !errors.As(err, &rejected) || rejected.Reason != agentv1.Reject_REASON_UNAUTHORIZED || rejected.Message != "revoked" {
			t.Fatalf("error = %v, want a RejectedError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept retrying after a rejection")
	}
}

// A refusal because of versions ends when the hub or the agent is updated, so
// the agent keeps asking instead of stopping for good.
func TestVersionRejectionIsRetried(t *testing.T) {
	for _, reason := range []agentv1.Reject_Reason{agentv1.Reject_REASON_UNSUPPORTED_PROTOCOL_VERSION, agentv1.Reject_REASON_AGENT_TOO_OLD} {
		welcomed := make(chan struct{})
		conn := newFakeHub(t, func(n int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if n < 3 {
				return stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Reject{Reject: &agentv1.Reject{Reason: reason, Message: "update me"}}})
			}
			if err := stream.Send(welcome("a", time.Hour, time.Hour)); err != nil {
				return err
			}
			close(welcomed)
			<-stream.Context().Done()
			return nil
		})
		_, res := runAgent(t, conn, opts())
		select {
		case <-welcomed:
		case err := <-res:
			t.Fatalf("%v: Run stopped with %v instead of trying again", reason, err)
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: the agent never got in", reason)
		}
	}
}

func TestReconnectsAndKeepsAssignedID(t *testing.T) {
	ids := make(chan string, 4)
	conn := newFakeHub(t, func(n int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		ids <- msg.GetHello().GetAgentId()
		if err := stream.Send(welcome("assigned-by-hub", time.Hour, time.Hour)); err != nil {
			return err
		}
		if n == 1 {
			return errors.New("hub restarting") // drops the first connection
		}
		<-stream.Context().Done()
		return nil
	})
	runAgent(t, conn, opts())

	for i, want := range []string{"", "assigned-by-hub"} {
		select {
		case got := <-ids:
			if got != want {
				t.Fatalf("connection %d: Hello agent id = %q, want %q", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("connection %d never happened", i+1)
		}
	}
}

func TestRunReturnsNilWhenCancelled(t *testing.T) {
	conn := newFakeHub(t, func(_ int, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
		if _, err := stream.Recv(); err != nil {
			return err
		}
		if err := stream.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		<-stream.Context().Done()
		return nil
	})
	cancel, res := runAgent(t, conn, opts())
	time.Sleep(50 * time.Millisecond) // let it connect
	cancel()

	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Run returned %v on cancellation, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestParsers(t *testing.T) {
	osRelease := "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\n"
	if got := parseOSRelease(osRelease); got != "Ubuntu 24.04.1 LTS" {
		t.Errorf("parseOSRelease = %q", got)
	}
	if got := parseOSRelease("ID=x\n"); got != "" {
		t.Errorf("parseOSRelease without PRETTY_NAME = %q, want empty", got)
	}

	meminfo := "MemTotal:       16318464 kB\nMemFree:         1234 kB\n"
	if got := parseMemTotal(meminfo); got != 16318464*1024 {
		t.Errorf("parseMemTotal = %d", got)
	}
	for _, bad := range []string{"", "MemTotal:\n", "MemTotal: lots kB\n"} {
		if got := parseMemTotal(bad); got != 0 {
			t.Errorf("parseMemTotal(%q) = %d, want 0", bad, got)
		}
	}

	if got := parseUptime("350735.47 234388.90\n"); got != 350735 {
		t.Errorf("parseUptime = %d", got)
	}
	for _, bad := range []string{"", "abc def", "-5 1"} {
		if got := parseUptime(bad); got != 0 {
			t.Errorf("parseUptime(%q) = %d, want 0", bad, got)
		}
	}
}
