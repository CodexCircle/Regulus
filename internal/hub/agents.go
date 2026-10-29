// agents.go is the hub side of the agent-hub contract: agentServer implements
// AgentService. Reading order: AgentInfo, agentServer and session, Connect (the
// handshake: readHello, version checks, register), serve and handle (heartbeats
// and metrics), sendCommand with its pending-result bookkeeping, and reject. See
// docs/code-map.md.

package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/protocol"
	"github.com/CodexCircle/Regulus/internal/update"
)

// Errors returned by SendCommand.
var (
	ErrAgentNotFound     = errors.New("agent is not connected")
	ErrAgentDisconnected = errors.New("agent disconnected before answering")
)

const (
	defaultHeartbeatInterval = 15 * time.Second
	defaultMetricsInterval   = 10 * time.Second
	defaultHelloTimeout      = 10 * time.Second

	// An agent that sends nothing for this many heartbeat intervals is dropped.
	missedHeartbeats = 3
)

// AgentInfo is a snapshot of a connected agent.
type AgentInfo struct {
	ID            string
	Version       string
	System        *agentv1.SystemInfo
	ConnectedAt   time.Time
	LastHeartbeat time.Time // zero until the first heartbeat
	LastMetrics   *agentv1.MetricsReport
	// ProtocolVersion is the version negotiated for the connection.
	ProtocolVersion uint32
	// Compat tells how the agent's release relates to the hub's.
	Compat update.Compat
}

// Rejection is the last time the hub refused an enrolled agent because its
// version cannot work with this hub. The dashboard shows it until the agent
// connects.
type Rejection struct {
	At      time.Time
	Version string
	Message string
}

// agentServer implements the hub side of the agent contract and keeps the
// registry of connected agents in memory.
type agentServer struct {
	agentv1.UnimplementedAgentServiceServer

	log               *slog.Logger
	heartbeatInterval time.Duration
	metricsInterval   time.Duration
	helloTimeout      time.Duration

	metrics *metricsStore
	events  *broker

	// hubVersion is the release of the hub, to judge the release of agents.
	hubVersion string

	// files, if set, serves the cached release to the agents (FetchUpdate).
	files updateFiles

	// seen, if set, is told when an agent connects and when it goes away.
	seen func(info AgentInfo, at time.Time)

	mu       sync.Mutex
	sessions map[string]*session
	rejected map[string]Rejection // by agent id, only for agents with a certificate
}

// updateFiles opens the files of a cached release; *update.Stager implements it.
type updateFiles interface {
	Open(version, name string) (*os.File, int64, error)
}

// session is one connected agent.
type session struct {
	info AgentInfo // guarded by agentServer.mu

	out      chan *agentv1.HubMessage
	replaced chan struct{} // closed when a newer connection takes over the agent id
	revoked  chan struct{} // closed when the agent is revoked
	revokeMu sync.Once
	done     chan struct{} // closed when the stream ends

	pendingMu sync.Mutex
	pending   map[string]chan *agentv1.CommandResult
	nextCmd   uint64
}

func newAgentServer(log *slog.Logger) *agentServer {
	return &agentServer{
		log:               log,
		heartbeatInterval: defaultHeartbeatInterval,
		metricsInterval:   defaultMetricsInterval,
		helloTimeout:      defaultHelloTimeout,
		metrics:           newMetricsStore(defaultMetricsRetention),
		events:            newBroker(),
		sessions:          map[string]*session{},
		rejected:          map[string]Rejection{},
	}
}

// Connect serves one agent stream: registration, then heartbeats, metrics and
// commands until the stream ends.
func (s *agentServer) Connect(stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	hello, err := s.readHello(ctx, stream)
	if err != nil {
		return err
	}
	negotiated, ok := protocol.Negotiate(hello.GetMinProtocolVersion(), hello.GetProtocolVersion())
	if !ok {
		msg := fmt.Sprintf("protocol version %d is not supported by this hub (supported: %d to %d): update the agent", hello.GetProtocolVersion(), protocol.MinSupported, protocol.Version)
		if hello.GetProtocolVersion() > protocol.Version {
			msg = fmt.Sprintf("the agent needs protocol version %d or newer but this hub speaks %d to %d: update the hub first", max(hello.GetMinProtocolVersion(), hello.GetProtocolVersion()), protocol.MinSupported, protocol.Version)
		}
		return s.reject(ctx, stream, hello, &agentv1.Reject{
			Reason:             agentv1.Reject_REASON_UNSUPPORTED_PROTOCOL_VERSION,
			Message:            msg,
			MinProtocolVersion: protocol.MinSupported,
			MaxProtocolVersion: protocol.Version,
		})
	}
	compat := update.CheckCompat(s.hubVersion, hello.GetAgentVersion())
	if compat == update.CompatTooOld {
		return s.reject(ctx, stream, hello, &agentv1.Reject{
			Reason:             agentv1.Reject_REASON_AGENT_TOO_OLD,
			Message:            fmt.Sprintf("agent version %s is too far behind this hub (%s): only the same release series and the previous one are supported; update the agent", hello.GetAgentVersion(), s.hubVersion),
			MinProtocolVersion: protocol.MinSupported,
			MaxProtocolVersion: protocol.Version,
		})
	}

	id := hello.GetAgentId()
	if who, ok := identityFrom(ctx); ok {
		// A remote agent is who its certificate says, whatever Hello claims.
		if id != "" && id != who.AgentID {
			return stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Reject{Reject: &agentv1.Reject{
				Reason:  agentv1.Reject_REASON_UNAUTHORIZED,
				Message: "the agent id does not match the client certificate",
			}}})
		}
		id = who.AgentID
	}
	if id == "" {
		if id, err = newAgentID(); err != nil {
			return status.Errorf(codes.Internal, "cannot assign an agent id: %v", err)
		}
	}
	sess := s.register(id, hello, negotiated, compat)
	defer s.unregister(sess)
	s.log.Info("agent connected", "agent", id, "version", hello.GetAgentVersion(), "host", hello.GetSystem().GetHostname())

	if err := stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Welcome{Welcome: &agentv1.Welcome{
		ProtocolVersion:   negotiated,
		AgentId:           id,
		HeartbeatInterval: durationpb.New(s.heartbeatInterval),
		MetricsInterval:   durationpb.New(s.metricsInterval),
	}}}); err != nil {
		return err
	}

	return s.serve(ctx, stream, sess)
}

// readHello waits for the first message, which must be Hello.
func (s *agentServer) readHello(ctx context.Context, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]) (*agentv1.Hello, error) {
	type result struct {
		msg *agentv1.AgentMessage
		err error
	}
	resc := make(chan result, 1)
	go func() {
		msg, err := stream.Recv()
		resc <- result{msg, err}
	}()

	timer := time.NewTimer(s.helloTimeout)
	defer timer.Stop()
	select {
	case r := <-resc:
		if r.err != nil {
			return nil, r.err
		}
		hello := r.msg.GetHello()
		if hello == nil {
			return nil, status.Error(codes.InvalidArgument, "the first message must be Hello")
		}
		return hello, nil
	case <-timer.C:
		return nil, status.Error(codes.DeadlineExceeded, "no Hello received")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// serve runs the message loop of a registered agent. Only this goroutine sends
// on the stream, which gRPC requires.
func (s *agentServer) serve(ctx context.Context, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage], sess *session) error {
	msgs := make(chan *agentv1.AgentMessage)
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

	idle := time.NewTimer(missedHeartbeats * s.heartbeatInterval)
	defer idle.Stop()

	for {
		select {
		case msg := <-msgs:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(missedHeartbeats * s.heartbeatInterval)
			if err := s.handle(sess, msg); err != nil {
				return err
			}
		case out := <-sess.out:
			if err := stream.Send(out); err != nil {
				return err
			}
		case err := <-recvErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case <-idle.C:
			s.log.Warn("dropping agent that stopped sending", "agent", sess.info.ID)
			return status.Error(codes.DeadlineExceeded, "no message received from the agent")
		case <-sess.replaced:
			return status.Error(codes.AlreadyExists, "replaced by a newer connection of the same agent")
		case <-sess.revoked:
			return status.Error(codes.PermissionDenied, "this agent was revoked")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *agentServer) handle(sess *session, msg *agentv1.AgentMessage) error {
	switch p := msg.GetPayload().(type) {
	case *agentv1.AgentMessage_Heartbeat:
		now := time.Now()
		s.mu.Lock()
		sess.info.LastHeartbeat = now
		s.mu.Unlock()
		s.events.publish(heartbeatEvent{Type: "agent.heartbeat", ID: sess.info.ID, At: now.UTC()})
	case *agentv1.AgentMessage_Metrics:
		s.mu.Lock()
		sess.info.LastMetrics = p.Metrics
		s.mu.Unlock()
		if sample, prev := s.metrics.add(sess.info.ID, p.Metrics); sample.Report != nil {
			s.events.publish(metricsEvent{Type: "agent.metrics", ID: sess.info.ID, Point: toPoint(sample, prev)})
		}
	case *agentv1.AgentMessage_CommandResult:
		sess.deliver(p.CommandResult)
	case *agentv1.AgentMessage_Hello:
		return status.Error(codes.InvalidArgument, "Hello was already received")
	}
	return nil
}

// register adds the agent to the registry. A previous connection with the same
// id is replaced: an agent that reconnects after a network failure must not be
// blocked by its own stale stream.
func (s *agentServer) register(id string, hello *agentv1.Hello, negotiated uint32, compat update.Compat) *session {
	sess := &session{
		info: AgentInfo{
			ID:              id,
			Version:         hello.GetAgentVersion(),
			System:          hello.GetSystem(),
			ConnectedAt:     time.Now(),
			ProtocolVersion: negotiated,
			Compat:          compat,
		},
		out:      make(chan *agentv1.HubMessage),
		replaced: make(chan struct{}),
		revoked:  make(chan struct{}),
		done:     make(chan struct{}),
		pending:  map[string]chan *agentv1.CommandResult{},
	}

	s.mu.Lock()
	old := s.sessions[id]
	s.sessions[id] = sess
	delete(s.rejected, id)
	s.mu.Unlock()

	if old != nil {
		close(old.replaced)
	}
	s.events.publish(agentEvent{Type: "agent.connected", Agent: toAgentJSON(sess.info, time.Now(), s.staleAfter())})
	if s.seen != nil {
		s.seen(sess.info, time.Now())
	}
	return sess
}

// disconnect ends the connection of a revoked agent.
func (s *agentServer) disconnect(id string) {
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	if sess != nil {
		sess.revokeMu.Do(func() { close(sess.revoked) })
	}
}

// staleAfter is how long an agent may stay silent before it is reported as
// stale: two heartbeat intervals, so one lost heartbeat is tolerated.
func (s *agentServer) staleAfter() time.Duration {
	return 2 * s.heartbeatInterval
}

func (s *agentServer) unregister(sess *session) {
	close(sess.done)
	s.mu.Lock()
	gone := s.sessions[sess.info.ID] == sess
	if gone {
		delete(s.sessions, sess.info.ID)
	}
	s.mu.Unlock()
	s.log.Info("agent disconnected", "agent", sess.info.ID)
	if s.seen != nil {
		s.mu.Lock()
		info := sess.info
		s.mu.Unlock()
		s.seen(info, time.Now())
	}
	if gone { // a replaced connection leaves the agent connected through the new one
		s.events.publish(goneEvent{Type: "agent.disconnected", ID: sess.info.ID})
	}
}

// agents returns a snapshot of the connected agents, ordered by id.
func (s *agentServer) agents() []AgentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]AgentInfo, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess.info)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// metricsHistory returns the recent metrics of an agent, oldest first. known is
// false when the hub has never seen the agent.
func (s *agentServer) metricsHistory(agentID string) (samples []MetricsSample, known bool) {
	s.mu.Lock()
	_, connected := s.sessions[agentID]
	s.mu.Unlock()
	return s.metrics.history(agentID), connected || s.metrics.known(agentID)
}

// sendCommand sends cmd to the agent and waits for its result. The command id
// is assigned here and overrides cmd.Id.
func (s *agentServer) sendCommand(ctx context.Context, agentID string, cmd *agentv1.Command) (*agentv1.CommandResult, error) {
	s.mu.Lock()
	sess := s.sessions[agentID]
	s.mu.Unlock()
	if sess == nil {
		return nil, ErrAgentNotFound
	}

	id, resc := sess.addPending()
	defer sess.removePending(id)

	out := &agentv1.Command{Id: id, Kind: cmd.GetKind()}
	select {
	case sess.out <- &agentv1.HubMessage{Payload: &agentv1.HubMessage_Command{Command: out}}:
	case <-sess.done:
		return nil, ErrAgentDisconnected
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case res := <-resc:
		return res, nil
	case <-sess.done:
		return nil, ErrAgentDisconnected
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (sess *session) addPending() (string, chan *agentv1.CommandResult) {
	sess.pendingMu.Lock()
	defer sess.pendingMu.Unlock()
	sess.nextCmd++
	id := fmt.Sprintf("cmd-%d", sess.nextCmd)
	ch := make(chan *agentv1.CommandResult, 1)
	sess.pending[id] = ch
	return id, ch
}

func (sess *session) removePending(id string) {
	sess.pendingMu.Lock()
	delete(sess.pending, id)
	sess.pendingMu.Unlock()
}

func (sess *session) deliver(res *agentv1.CommandResult) {
	sess.pendingMu.Lock()
	ch := sess.pending[res.GetCommandId()]
	sess.pendingMu.Unlock()
	if ch == nil {
		return // late or unknown result
	}
	select {
	case ch <- res:
	default:
	}
}

func newAgentID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "agent-" + hex.EncodeToString(b), nil
}

// reject tells the agent why it is refused, remembers it for the dashboard when
// the agent is known by its certificate, and ends the stream.
func (s *agentServer) reject(ctx context.Context, stream grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage], hello *agentv1.Hello, r *agentv1.Reject) error {
	s.log.Warn("rejecting agent", "reason", r.GetReason().String(), "protocol_version", hello.GetProtocolVersion(), "agent_version", hello.GetAgentVersion())
	if who, ok := identityFrom(ctx); ok {
		s.mu.Lock()
		if len(s.rejected) < maxRejections {
			s.rejected[who.AgentID] = Rejection{At: time.Now(), Version: hello.GetAgentVersion(), Message: r.GetMessage()}
		}
		s.mu.Unlock()
	}
	return stream.Send(&agentv1.HubMessage{Payload: &agentv1.HubMessage_Reject{Reject: r}})
}

// maxRejections bounds the memory used to remember refused agents.
const maxRejections = 1000

// Rejection returns why an enrolled agent was last refused, if it has not
// connected since.
func (s *agentServer) rejection(id string) (Rejection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rejected[id]
	return r, ok
}
