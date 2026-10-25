// Package agent is the client side of the agent-hub contract. The same code
// runs inside the hub (the built-in agent, over the in-process transport) and,
// later, on remote servers.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/protocol"
)

const (
	// Used when the hub does not say how often to report.
	defaultHeartbeatInterval = 15 * time.Second
	defaultMetricsInterval   = 10 * time.Second

	// The smallest interval a hub may ask for.
	minInterval = 10 * time.Millisecond

	collectTimeout = 5 * time.Second
)

// Options configure Run.
type Options struct {
	// Version is the agent release version sent in Hello.
	Version string
	// ID is the agent id. Leave it empty to let the hub assign one; the
	// assigned id is reused when the agent reconnects.
	ID string
	// Collector gathers metrics. Defaults to the Linux collector (CPU, memory,
	// disks, network, uptime), or to one that reports only the time and the
	// uptime where /proc is not available.
	Collector Collector
	// Log defaults to slog.Default().
	Log *slog.Logger
	// MinBackoff and MaxBackoff bound the wait between reconnection attempts
	// (defaults 1s and 30s).
	MinBackoff, MaxBackoff time.Duration
}

// RejectedError is returned by Run when the hub refuses the agent. Retrying
// would not help, so Run does not.
type RejectedError struct {
	Reason  agentv1.Reject_Reason
	Message string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("rejected by the hub (%s): %s", e.Reason, e.Message)
}

type agent struct {
	opts   Options
	log    *slog.Logger
	client agentv1.AgentServiceClient
	system *agentv1.SystemInfo
	id     string
}

// Run connects to the hub over conn and serves it until ctx is cancelled, in
// which case it returns nil. Connection failures are retried with exponential
// backoff; a rejection by the hub is returned as a *RejectedError.
func Run(ctx context.Context, conn grpc.ClientConnInterface, opts Options) error {
	if opts.Collector == nil {
		opts.Collector = defaultCollector()
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = time.Second
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(30*time.Second, opts.MinBackoff)
	}

	a := &agent{
		opts:   opts,
		log:    opts.Log,
		client: agentv1.NewAgentServiceClient(conn),
		system: systemInfo(),
		id:     opts.ID,
	}

	backoff := opts.MinBackoff
	for {
		established, err := a.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		var rejected *RejectedError
		if errors.As(err, &rejected) {
			return err
		}
		a.log.Warn("connection to the hub lost", "err", err, "retry_in", backoff)
		if established {
			backoff = opts.MinBackoff
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		backoff = min(backoff*2, opts.MaxBackoff)
	}
}

// session runs one connection to the hub. established tells whether the hub
// accepted the agent before the session ended.
func (a *agent) session(ctx context.Context) (established bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := a.client.Connect(ctx)
	if err != nil {
		return false, err
	}
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		ProtocolVersion: protocol.Version,
		AgentVersion:    a.opts.Version,
		AgentId:         a.id,
		System:          a.system,
	}}}); err != nil {
		return false, err
	}

	first, err := stream.Recv()
	if err != nil {
		return false, err
	}
	var welcome *agentv1.Welcome
	switch p := first.GetPayload().(type) {
	case *agentv1.HubMessage_Welcome:
		welcome = p.Welcome
	case *agentv1.HubMessage_Reject:
		return false, &RejectedError{Reason: p.Reject.GetReason(), Message: p.Reject.GetMessage()}
	default:
		return false, errors.New("the hub did not answer Hello with Welcome or Reject")
	}
	if welcome.GetAgentId() != "" {
		a.id = welcome.GetAgentId()
	}
	a.log.Info("connected to the hub", "agent", a.id, "protocol_version", welcome.GetProtocolVersion())

	heartbeat := time.NewTicker(intervalOr(welcome.GetHeartbeatInterval().AsDuration(), defaultHeartbeatInterval))
	defer heartbeat.Stop()
	metrics := time.NewTicker(intervalOr(welcome.GetMetricsInterval().AsDuration(), defaultMetricsInterval))
	defer metrics.Stop()

	msgs := make(chan *agentv1.HubMessage)
	recvErr := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			select {
			case msgs <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Report right away so the hub has data without waiting for a full interval.
	if err := a.sendHeartbeat(stream); err != nil {
		return true, err
	}
	if err := a.sendMetrics(ctx, stream); err != nil {
		return true, err
	}

	// Only this goroutine sends on the stream, which gRPC requires.
	for {
		select {
		case <-heartbeat.C:
			if err := a.sendHeartbeat(stream); err != nil {
				return true, err
			}
		case <-metrics.C:
			if err := a.sendMetrics(ctx, stream); err != nil {
				return true, err
			}
		case msg := <-msgs:
			cmd := msg.GetCommand()
			if cmd == nil {
				continue // nothing else is defined for the hub to send after Welcome
			}
			res := a.runCommand(cmd, heartbeat, metrics)
			if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_CommandResult{CommandResult: res}}); err != nil {
				return true, err
			}
		case err := <-recvErr:
			return true, err
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
}

func (a *agent) sendHeartbeat(stream agentv1.AgentService_ConnectClient) error {
	return stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{
		Heartbeat: &agentv1.Heartbeat{SentAt: timestamppb.Now()},
	}})
}

// sendMetrics collects and sends a report. A failing collector is logged and
// skipped: it must not take the connection down.
func (a *agent) sendMetrics(ctx context.Context, stream agentv1.AgentService_ConnectClient) error {
	collectCtx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()
	report, err := a.opts.Collector.Collect(collectCtx)
	if err != nil {
		a.log.Warn("collecting metrics failed", "err", err)
		return nil
	}
	return stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: report}})
}

func (a *agent) runCommand(cmd *agentv1.Command, heartbeat, metrics *time.Ticker) *agentv1.CommandResult {
	res := &agentv1.CommandResult{CommandId: cmd.GetId()}
	switch k := cmd.GetKind().(type) {
	case *agentv1.Command_Ping:
		res.Success = true
	case *agentv1.Command_SetIntervals:
		h := k.SetIntervals.GetHeartbeatInterval().AsDuration()
		m := k.SetIntervals.GetMetricsInterval().AsDuration()
		// Zero keeps the current interval; anything else must be sane.
		if (h != 0 && h < minInterval) || (m != 0 && m < minInterval) {
			res.Error = fmt.Sprintf("intervals must be at least %v", minInterval)
			break
		}
		if h != 0 {
			heartbeat.Reset(h)
		}
		if m != 0 {
			metrics.Reset(m)
		}
		res.Success = true
	default:
		res.Error = "unsupported command"
	}
	return res
}

func intervalOr(d, fallback time.Duration) time.Duration {
	if d < minInterval {
		return fallback
	}
	return d
}
