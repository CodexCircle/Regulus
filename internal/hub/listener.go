package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/CodexCircle/Regulus/internal/auth"
)

// The listener for remote agents is turned on either by the configuration
// (agent_listen, which wins and cannot be changed from the dashboard) or from
// the dashboard, which keeps the address in the database so it survives a
// restart.
const (
	// settingAgentListen is the database setting saved by the dashboard.
	settingAgentListen = "agents.listen"

	// EventAgentsListenerOn is the audit event of turning remote agents on.
	EventAgentsListenerOn = "agents_listener_on"

	// DefaultAgentPort is the port proposed for remote agents.
	DefaultAgentPort = 8443
)

// Where the listener address comes from.
const (
	ListenerFromConfig    = "config"
	ListenerFromDashboard = "dashboard"
)

// Errors of EnableAgents.
var (
	ErrAgentsOn        = errors.New("remote agents are already on")
	ErrAgentsInConfig  = errors.New("agent_listen is set in the configuration: change it there")
	ErrInvalidPort     = errors.New("the port must be a number from 1024 to 65535")
	ErrHubNotServing   = errors.New("the hub is not serving yet")
	errListenerBlocked = errors.New("the port cannot be opened")
)

// AgentListener is the state of the listener for remote agents.
type AgentListener struct {
	// Enabled is true while remote agents can connect.
	Enabled bool `json:"enabled"`
	// Port is where they connect, when enabled.
	Port int `json:"port,omitempty"`
	// Source is ListenerFromConfig or ListenerFromDashboard, when enabled.
	Source string `json:"source,omitempty"`
	// CAFingerprint is what an agent pins to trust this hub.
	CAFingerprint string `json:"ca_fingerprint"`
	// Editable is true when the dashboard may turn the listener on.
	Editable bool `json:"editable"`
}

// listenerState guards the listener started by Serve or by EnableAgents.
type listenerState struct {
	mu      sync.Mutex
	serving bool
	source  string
	stop    func()
}

// startAgentListener starts the listener for remote agents at Serve: from the
// configuration if set, else from the address the dashboard saved.
func (h *Hub) startAgentListener(ctx context.Context) error {
	addr, source := h.cfg.AgentListen, ListenerFromConfig
	if addr == "" {
		saved, err := h.savedAgentListen(ctx)
		if err != nil {
			return err
		}
		addr, source = saved, ListenerFromDashboard
	}
	h.listener.mu.Lock()
	defer h.listener.mu.Unlock()
	h.listener.serving = true
	if addr == "" {
		return nil
	}
	stop, err := h.serveAgents(addr)
	if err != nil {
		if source == ListenerFromConfig {
			return err
		}
		// A port chosen from the dashboard must never keep the hub from
		// starting: remote agents stay off and the dashboard offers them again.
		h.log.Error("remote agents stay off: the port chosen in the dashboard cannot be opened", "addr", addr, "err", err)
		return nil
	}
	h.listener.stop, h.listener.source = stop, source
	return nil
}

// stopAgentListener stops the listener when Serve returns.
func (h *Hub) stopAgentListener() {
	h.listener.mu.Lock()
	defer h.listener.mu.Unlock()
	h.listener.serving = false
	if h.listener.stop != nil {
		h.listener.stop()
		h.listener.stop = nil
	}
	h.agentAddr.Store("")
}

// AgentListener reports whether remote agents can connect, and where.
func (h *Hub) AgentListener(context.Context) (AgentListener, error) {
	h.listener.mu.Lock()
	source := h.listener.source
	h.listener.mu.Unlock()
	st := AgentListener{CAFingerprint: h.fleet.CA().Fingerprint(), Editable: h.cfg.AgentListen == ""}
	if addr := h.AgentAddr(); addr != "" {
		_, port, _ := net.SplitHostPort(addr)
		st.Enabled = true
		st.Port, _ = strconv.Atoi(port)
		st.Source = source
		st.Editable = false
	}
	return st, nil
}

// EnableAgents turns remote agents on at port, on every address of the
// server, and keeps the choice for the next start. It is refused when the
// configuration sets agent_listen, which always wins.
func (h *Hub) EnableAgents(ctx context.Context, by string, port int) (AgentListener, error) {
	if port < 1024 || port > 65535 {
		return AgentListener{}, ErrInvalidPort
	}
	if h.cfg.AgentListen != "" {
		return AgentListener{}, ErrAgentsInConfig
	}
	addr := ":" + strconv.Itoa(port)
	h.listener.mu.Lock()
	switch {
	case !h.listener.serving:
		h.listener.mu.Unlock()
		return AgentListener{}, ErrHubNotServing
	case h.listener.stop != nil:
		h.listener.mu.Unlock()
		return AgentListener{}, ErrAgentsOn
	}
	stop, err := h.serveAgents(addr)
	if err != nil {
		h.listener.mu.Unlock()
		return AgentListener{}, fmt.Errorf("%w: %w", errListenerBlocked, err)
	}
	h.listener.stop, h.listener.source = stop, ListenerFromDashboard
	h.listener.mu.Unlock()

	if err := h.saveAgentListen(ctx, addr); err != nil {
		h.log.Error("saving the agent listener", "err", err)
	}
	h.auth.Record(ctx, by, EventAgentsListenerOn, "agents", "port "+strconv.Itoa(port))
	return h.AgentListener(ctx)
}

func (h *Hub) savedAgentListen(ctx context.Context) (string, error) {
	var v string
	err := h.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingAgentListen).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (h *Hub) saveAgentListen(ctx context.Context, addr string) error {
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		settingAgentListen, addr)
	return err
}

// listenerHandlers serve GET and PUT /api/agents/listener.
type listenerHandlers struct {
	fleet FleetControl
}

func (l listenerHandlers) get(w http.ResponseWriter, r *http.Request) {
	if l.fleet == nil {
		http.NotFound(w, r)
		return
	}
	st, err := l.fleet.AgentListener(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSONBody(w, http.StatusOK, st)
}

func (l listenerHandlers) put(w http.ResponseWriter, r *http.Request) {
	if l.fleet == nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Port int `json:"port"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	user, _ := auth.UserFrom(r.Context())
	st, err := l.fleet.EnableAgents(r.Context(), user.Username, req.Port)
	switch {
	case errors.Is(err, ErrInvalidPort):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrAgentsOn), errors.Is(err, ErrAgentsInConfig), errors.Is(err, ErrHubNotServing):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errListenerBlocked):
		writeJSONError(w, http.StatusConflict, err.Error()+": is another program using it?")
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSONBody(w, http.StatusOK, st)
	}
}
