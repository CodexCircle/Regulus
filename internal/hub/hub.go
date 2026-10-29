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
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/fleet"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/inproc"
	"github.com/CodexCircle/Regulus/internal/pki"
	"github.com/CodexCircle/Regulus/internal/storage"
	"github.com/CodexCircle/Regulus/internal/update"
)

// ReleaseRepo is the repository the hub reads releases from.
const ReleaseRepo = "CodexCircle/Regulus"

const shutdownTimeout = 10 * time.Second

// Hub is a running-capable hub instance: its configuration, database, UI and
// the agents connected to it.
type Hub struct {
	cfg config.Config
	log *slog.Logger
	db  *sql.DB
	ui  fs.FS

	version   string
	startedAt time.Time
	auth      *auth.Service
	fleet     *fleet.Service
	updates   *update.Manager
	rollout   *rolloutState
	updateSrc update.Source   // nil: the GitHub releases of ReleaseRepo
	updateVer update.Verifier // nil: the Sigstore signature of the release workflow
	agentAddr atomic.Value    // string: where the agent listener is bound, once it is
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

// WithUpdateSource replaces the place the hub looks for new releases. Mostly
// useful in tests.
func WithUpdateSource(src update.Source) Option {
	return func(h *Hub) { h.updateSrc = src }
}

// WithUpdateVerifier replaces the check of the signature of downloaded
// releases. Only for tests: a hub without it never trusts an unsigned release.
func WithUpdateVerifier(v update.Verifier) Option {
	return func(h *Hub) { h.updateVer = v }
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

	secret, err := auth.LoadOrCreateSecret(cfg.DataDir)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("hub secret: %w", err)
	}
	authn, err := auth.New(ctx, db, log, auth.WithSecret(secret))
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	ca, err := pki.LoadOrCreate(filepath.Join(cfg.DataDir, "pki"), time.Now())
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("certificate authority: %w", err)
	}

	h := &Hub{
		cfg:       cfg,
		auth:      authn,
		fleet:     fleet.New(db, ca, authn),
		log:       log,
		db:        db,
		ui:        ui,
		agents:    newAgentServer(log),
		transport: inproc.New(),
		startedAt: time.Now(),
	}
	h.agents.seen = func(info AgentInfo, at time.Time) {
		sys := info.System
		if err := h.fleet.Seen(context.Background(), info.ID, info.Version, sys.GetHostname(), sys.GetOsVersion(), at); err != nil {
			log.Warn("recording when an agent was seen", "agent", info.ID, "err", err)
		}
	}
	if cfg.MetricsInterval > 0 {
		h.agents.metricsInterval = cfg.MetricsInterval
	}
	for _, opt := range opts {
		opt(h)
	}
	h.agents.hubVersion = h.version
	ver := h.updateVer
	if ver == nil {
		ver = &update.SigstoreVerifier{Trust: update.PublicTrust(filepath.Join(cfg.DataDir, "tuf"))}
	}
	src := h.updateSrc
	if src == nil {
		src = &update.GitHubSource{Repo: ReleaseRepo, UserAgent: "regulus-hub/" + h.version}
	}
	applyDir := ""
	if cfg.Updates.Apply {
		applyDir = update.ApplyDir(cfg.DataDir)
	}
	h.rollout = newRolloutState()
	stager := &update.Stager{
		Dir:      filepath.Join(cfg.DataDir, "updates"),
		Verifier: ver,
	}
	if _, ok := update.ParseBuildVersion(h.version); ok {
		stager.Keep = h.version
	}
	h.agents.files = stager
	h.updates = update.New(update.Options{
		ApplyDir: applyDir,
		DB:       db,
		Log:      log.With("component", "updates"),
		Source:   src,
		Stager:   stager,
		Audit:    authn,
		Version:  h.version,
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Enabled:  cfg.Updates.Check,
	})
	return h, nil
}

// Rejection returns why an enrolled agent was last refused because of its
// version, if it has not connected since.
func (h *Hub) Rejection(id string) (Rejection, bool) {
	return h.agents.rejection(id)
}

// Updates returns the manager that looks for new releases.
func (h *Hub) Updates() *update.Manager { return h.updates }

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
	h.rollout.stop()
	h.updates.Close()
	return h.db.Close()
}

// Serve serves HTTP on ln until ctx is cancelled, then shuts down gracefully.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           NewHandler(Deps{UI: h.ui, Agents: h, Layouts: h, Log: h.log, Auth: h.auth, Fleet: h, Updates: h.updates, Rollout: h}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The built-in agent talks to the agent service over the in-process
	// transport; remote agents use the network listener below.
	grpcSrv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(grpcSrv, h.agents)
	go func() { _ = grpcSrv.Serve(h.transport.Listener()) }()
	defer grpcSrv.Stop() // agent streams are long-lived, so a graceful stop would wait forever

	if h.cfg.AgentListen != "" {
		stopNet, err := h.serveAgents()
		if err != nil {
			return err
		}
		defer stopNet()
	}

	if h.cfg.BuiltinAgent {
		stopAgent, err := h.startBuiltinAgent(ctx)
		if err != nil {
			return err
		}
		defer stopAgent()
	}

	defer h.serveLocal(ln.Addr().String())()

	h.rollout.mu.Lock()
	h.rollout.base = ctx
	h.rollout.mu.Unlock()
	rolloutTick := time.NewTicker(5 * time.Minute)
	defer rolloutTick.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-rolloutTick.C:
				h.autoRollout(ctx)
			}
		}
	}()

	updatesDone := make(chan struct{})
	go func() {
		defer close(updatesDone)
		h.updates.Run(ctx)
	}()
	defer func() { <-updatesDone }()

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

// serveAgents starts the listener for remote agents: gRPC over TLS 1.3 with
// the hub's own CA. Streams need the client certificate of an enrolled agent;
// only Enroll works without one. The returned function stops it.
func (h *Hub) serveAgents() (stop func(), err error) {
	cert, err := h.fleet.CA().ServerCertificate(time.Now())
	if err != nil {
		return nil, fmt.Errorf("agent listener certificate: %w", err)
	}
	ln, err := net.Listen("tcp", h.cfg.AgentListen)
	if err != nil {
		return nil, fmt.Errorf("agent listener: %w", err)
	}
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(h.fleet.CA().ServerTLS(cert))),
		grpc.StreamInterceptor(h.requireAgent),
	)
	agentv1.RegisterAgentServiceServer(srv, h.agents)
	agentv1.RegisterEnrollmentServiceServer(srv, enrollServer{hub: h})
	go func() { _ = srv.Serve(ln) }()
	h.agentAddr.Store(ln.Addr().String())
	h.log.Info("agent listener ready", "addr", ln.Addr().String(), "ca", h.fleet.CA().Fingerprint())
	return srv.Stop, nil
}

// AgentAddr is where the listener for remote agents is bound, or "" while it
// is off.
func (h *Hub) AgentAddr() string {
	addr, _ := h.agentAddr.Load().(string)
	return addr
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
// stale.
func (h *Hub) StaleAfter() time.Duration {
	return h.agents.staleAfter()
}

// Subscribe returns a channel of the JSON events pushed on GET /api/stream and
// a function that ends the subscription.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	return h.agents.events.subscribe()
}

// Deps are what the HTTP handler serves.
type Deps struct {
	// UI holds the compiled dashboard.
	UI fs.FS
	// Agents, Layouts and Log may be nil in tests; the agent list is then empty
	// and the layout endpoints answer 404.
	Agents  AgentSource
	Layouts LayoutStore
	Log     *slog.Logger
	// Auth protects the API. A nil Auth leaves it open, which is only meant for
	// tests.
	Auth *auth.Service
	// Fleet enrolls and revokes remote agents. May be nil in tests; the
	// endpoints then answer 404.
	Fleet FleetControl
	// Updates reports and steers the updates of the hub. May be nil in tests;
	// the endpoints then answer 404.
	Updates UpdateControl
	// Rollout updates the managed servers to the version of the hub. May be nil
	// in tests; the endpoints then answer 404.
	Rollout RolloutControl
}

// route is a protected API endpoint.
type route struct {
	pattern string
	handler http.Handler
	// needsAuth marks the account endpoints, which only exist when the hub
	// has an auth service.
	needsAuth bool
}

// apiRoutes lists the endpoints under /api/ that need a login, other than the
// ones of the auth package. They must all be described in openapi.yaml.
func apiRoutes(d Deps) []route {
	h := layoutHandlers{layouts: d.Layouts, log: d.Log}
	f := fleetHandlers{fleet: d.Fleet}
	u := updateHandlers{updates: d.Updates}
	r := rolloutHandlers{rollout: d.Rollout}
	// Without authentication (tests that exercise the handlers alone) there is
	// no user to check.
	need := func(permission string, next http.Handler) http.Handler {
		if d.Auth == nil {
			return next
		}
		return auth.RequirePermission(permission, next)
	}
	routes := []route{
		{pattern: "GET /api/agents", handler: need(auth.PermServersRead, agentsHandler(d.Agents))},
		{pattern: "GET /api/agents/{id}", handler: need(auth.PermServersRead, agentGetHandler(d.Agents))},
		{pattern: "GET /api/agents/{id}/metrics", handler: need(auth.PermServersRead, metricsHandler(d.Agents))},
		{pattern: "GET /api/layout", handler: http.HandlerFunc(h.get)},
		{pattern: "PUT /api/layout", handler: need(auth.PermLayoutEdit, auth.RequireJSON(http.HandlerFunc(h.put)))},
		{pattern: "GET /api/stream", handler: need(auth.PermServersRead, streamHandler(d.Agents))},
		{pattern: "POST /api/enrollment-tokens", handler: need(auth.PermServersManage, auth.RequireJSON(http.HandlerFunc(f.createToken))), needsAuth: true},
		{pattern: "PATCH /api/agents/{id}", handler: need(auth.PermServersManage, auth.RequireJSON(http.HandlerFunc(f.rename))), needsAuth: true},
		{pattern: "DELETE /api/agents/{id}", handler: need(auth.PermServersManage, auth.RequireJSON(http.HandlerFunc(f.revoke))), needsAuth: true},
		{pattern: "GET /api/updates", handler: need(auth.PermCoreUpdate, http.HandlerFunc(u.status)), needsAuth: true},
		{pattern: "PUT /api/updates/settings", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(u.settings))), needsAuth: true},
		{pattern: "POST /api/updates/check", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(u.check))), needsAuth: true},
		{pattern: "POST /api/updates/download", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(u.download))), needsAuth: true},
		{pattern: "GET /api/updates/rollout", handler: need(auth.PermCoreUpdate, http.HandlerFunc(r.status)), needsAuth: true},
		{pattern: "POST /api/updates/rollout", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(r.start))), needsAuth: true},
		{pattern: "POST /api/updates/rollout/stop", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(r.stop))), needsAuth: true},
		{pattern: "POST /api/updates/apply", handler: need(auth.PermCoreUpdate, auth.RequireJSON(http.HandlerFunc(u.apply))), needsAuth: true},
		{pattern: "GET /api/openapi.yaml", handler: http.HandlerFunc(serveOpenAPI)},
	}
	for _, r := range auth.APIRoutes(d.Auth) {
		routes = append(routes, route{pattern: r.Pattern, handler: r.Handler, needsAuth: true})
	}
	return routes
}

// NewHandler returns the hub HTTP handler. The API (under /api/) requires a
// login, except for the endpoints of the auth package. The dashboard files and
// /healthz are public: they hold no data, and the page asks for a login before
// it shows anything.
func NewHandler(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	ui := d.UI
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})

	api := http.NewServeMux()
	for _, r := range apiRoutes(d) {
		if r.needsAuth && d.Auth == nil {
			continue
		}
		api.Handle(r.pattern, r.handler)
	}
	var protected http.Handler = api
	if d.Auth != nil {
		d.Auth.Register(mux)
		protected = d.Auth.Require(api)
	}
	mux.Handle("/api/", protected)

	files := http.FileServerFS(ui)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			http.Error(w, "dashboard not built: run `make dashboard` before `make build`", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
	return mux
}
