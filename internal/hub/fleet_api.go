package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/fleet"
)

// ErrAgentsOff is returned when remote agents are not enabled.
var ErrAgentsOff = errors.New("remote agents are off: set agent_listen in the configuration")

// Enrollment is what an admin needs to add a server: the token, valid once,
// and what the agent must trust.
type Enrollment struct {
	Token         string
	Name          string
	ExpiresAt     time.Time
	CAFingerprint string
	AgentPort     int
}

// FleetControl is what the HTTP API needs to enroll and revoke remote agents.
// *Hub implements it.
type FleetControl interface {
	CreateEnrollment(ctx context.Context, by, name string) (Enrollment, error)
	RevokeAgent(ctx context.Context, by, id string) error
	RenameAgent(ctx context.Context, by, id, name string) error
}

// CreateEnrollment makes a one-time enrollment token.
func (h *Hub) CreateEnrollment(ctx context.Context, by, name string) (Enrollment, error) {
	addr := h.AgentAddr()
	if addr == "" {
		return Enrollment{}, ErrAgentsOff
	}
	token, expires, err := h.fleet.CreateToken(ctx, by, name)
	if err != nil {
		return Enrollment{}, err
	}
	_, port, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(port)
	return Enrollment{
		Token:         token,
		Name:          name,
		ExpiresAt:     expires,
		CAFingerprint: h.fleet.CA().Fingerprint(),
		AgentPort:     n,
	}, nil
}

// RevokeAgent stops an agent from connecting again and drops its connection.
func (h *Hub) RevokeAgent(ctx context.Context, by, id string) error {
	if err := h.fleet.Revoke(ctx, by, id); err != nil {
		return err
	}
	h.agents.disconnect(id)
	return nil
}

// RenameAgent sets the name shown for an enrolled agent.
func (h *Hub) RenameAgent(ctx context.Context, by, id, name string) error {
	return h.fleet.Rename(ctx, by, id, name)
}

// Registry lists the enrolled agents, connected or not.
func (h *Hub) Registry(ctx context.Context) ([]fleet.Agent, error) {
	return h.fleet.Agents(ctx)
}

type fleetHandlers struct {
	fleet FleetControl
}

func (f fleetHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	if f.fleet == nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	grant, err := f.fleet.CreateEnrollment(r.Context(), by.Username, req.Name)
	var nameErr *fleet.NameError
	switch {
	case errors.Is(err, ErrAgentsOff):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.As(err, &nameErr):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSONBody(w, http.StatusCreated, map[string]any{
			"token":          grant.Token,
			"name":           grant.Name,
			"expires_at":     grant.ExpiresAt.UTC(),
			"ca_fingerprint": grant.CAFingerprint,
			"agent_port":     grant.AgentPort,
		})
	}
}

func (f fleetHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	if f.fleet == nil {
		http.NotFound(w, r)
		return
	}
	var req struct{}
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	err := f.fleet.RevokeAgent(r.Context(), by.Username, r.PathValue("id"))
	switch {
	case errors.Is(err, fleet.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "no enrolled agent with this id")
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodeBody reads a small JSON request body; it answers 400 and returns false
// when the body is not valid.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (f fleetHandlers) rename(w http.ResponseWriter, r *http.Request) {
	if f.fleet == nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	err := f.fleet.RenameAgent(r.Context(), by.Username, r.PathValue("id"), req.Name)
	var nameErr *fleet.NameError
	switch {
	case errors.Is(err, fleet.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "no enrolled agent with this id")
	case errors.As(err, &nameErr):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
