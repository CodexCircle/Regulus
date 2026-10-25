// Package hub implements the Regulus hub: the HTTP server that serves the
// dashboard and, in later phases, the API and the agent connections.
package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/inproc"
	"github.com/CodexCircle/Regulus/internal/storage"
)

const shutdownTimeout = 10 * time.Second

// Hub is a running-capable hub instance: its configuration, database, UI and
// the agents connected to it.
type Hub struct {
	cfg config.Config
	log *slog.Logger
	db  *sql.DB
	ui  fs.FS

	version   string
	agents    *agentServer
	transport *inproc.Transport // lets the built-in agent reach the hub without a network port
}

// Option customizes a Hub.
type Option func(*Hub)

// WithVersion sets the release version of the hub, reported by the built-in
// agent.
func WithVersion(version string) Option {
	return func(h *Hub) { h.version = version }
}

// WithAgentIntervals sets the heartbeat and metrics intervals the hub asks
// agents to use. Mostly useful in tests.
func WithAgentIntervals(heartbeat, metrics time.Duration) Option {
	return func(h *Hub) {
		h.agents.heartbeatInterval = heartbeat
		h.agents.metricsInterval = metrics
	}
}

// New opens the database (applying migrations) and returns a Hub that serves
// the dashboard from ui. Call Close when done.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, ui fs.FS, opts ...Option) (*Hub, error) {
	db, err := storage.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	log.Info("database ready", "dir", cfg.DataDir)

	h := &Hub{
		cfg:       cfg,
		log:       log,
		db:        db,
		ui:        ui,
		agents:    newAgentServer(log),
		transport: inproc.New(),
	}
	if cfg.MetricsInterval > 0 {
		h.agents.metricsInterval = cfg.MetricsInterval
	}
	for _, opt := range opts {
		opt(h)
	}
	return h, nil
}

// Agents returns a snapshot of the connected agents, ordered by id.
func (h *Hub) Agents() []AgentInfo {
	return h.agents.agents()
}

// Metrics returns the recent metrics reported by an agent, oldest first. known
// is false when the hub has never seen the agent.
func (h *Hub) Metrics(agentID string) (samples []MetricsSample, known bool) {
	return h.agents.metricsHistory(agentID)
}

// SendCommand sends a command to a connected agent and waits for its result.
// It returns ErrAgentNotFound if the agent is not connected and
// ErrAgentDisconnected if it goes away before answering.
func (h *Hub) SendCommand(ctx context.Context, agentID string, cmd *agentv1.Command) (*agentv1.CommandResult, error) {
	return h.agents.sendCommand(ctx, agentID, cmd)
}

// AgentConn returns a client connection to the hub's agent service over the
// in-process transport. The built-in agent uses it. Close it when done.
func (h *Hub) AgentConn() (*grpc.ClientConn, error) {
	return h.transport.Dial()
}

// Close releases the hub resources.
func (h *Hub) Close() error {
	return h.db.Close()
}

// Serve serves HTTP on ln until ctx is cancelled, then shuts down gracefully.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           NewHandler(h.ui, h),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The agent service runs on the in-process transport; the network
	// listener for remote agents comes with the remote agent work.
	grpcSrv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(grpcSrv, h.agents)
	go func() { _ = grpcSrv.Serve(h.transport.Listener()) }()
	defer grpcSrv.Stop() // agent streams are long-lived, so a graceful stop would wait forever

	if h.cfg.BuiltinAgent {
		stopAgent, err := h.startBuiltinAgent(ctx)
		if err != nil {
			return err
		}
		defer stopAgent()
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	h.log.Info("hub listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	h.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// BuiltinAgentID is the id of the agent that runs inside the hub.
const BuiltinAgentID = "local"

// startBuiltinAgent runs the agent of the hub's own server. It talks to the
// hub through the same contract as any remote agent, over the in-process
// transport. The returned function stops it and waits for it to finish.
func (h *Hub) startBuiltinAgent(ctx context.Context) (stop func(), err error) {
	conn, err := h.AgentConn()
	if err != nil {
		return nil, fmt.Errorf("built-in agent: %w", err)
	}
	agentCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := agent.Run(agentCtx, conn, agent.Options{
			Version: h.version,
			ID:      BuiltinAgentID,
			Log:     h.log.With("component", "builtin-agent"),
		})
		if err != nil {
			h.log.Error("built-in agent stopped", "err", err)
		}
	}()
	h.log.Info("built-in agent enabled")
	return func() {
		cancel()
		<-done
		_ = conn.Close()
	}, nil
}

// StaleAfter is how long an agent may stay silent before it is reported as
// stale: two heartbeat intervals, so one lost heartbeat is tolerated.
func (h *Hub) StaleAfter() time.Duration {
	return 2 * h.agents.heartbeatInterval
}

// NewHandler returns the hub HTTP handler. The dashboard is served from ui and
// the agents API from agents, which may be nil (the list is then empty).
func NewHandler(ui fs.FS, agents AgentSource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("GET /api/agents", agentsHandler(agents))
	mux.HandleFunc("GET /api/agents/{id}/metrics", metricsHandler(agents))

	files := http.FileServerFS(ui)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			http.Error(w, "dashboard not built: run `make dashboard` before `make build`", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
	return mux
}
