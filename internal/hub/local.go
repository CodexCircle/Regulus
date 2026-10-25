package hub

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/CodexCircle/Regulus/internal/localstatus"
)

// maxSocketPath is the longest path a Unix socket can have on Linux.
const maxSocketPath = 107

// localStatus describes the running hub for the tools on the same machine.
func (h *Hub) localStatus(addr string) localstatus.Status {
	st := localstatus.Status{
		Version:      h.version,
		StartedAt:    h.startedAt,
		Addr:         addr,
		AgentAddr:    h.AgentAddr(),
		BuiltinAgent: h.cfg.BuiltinAgent,
		SetupToken:   h.auth.SetupToken(),
		Agents:       []localstatus.Agent{},
	}
	now := time.Now()
	for _, a := range h.Agents() {
		j := toAgentJSON(a, now, h.StaleAfter())
		agent := localstatus.Agent{ID: j.ID, Hostname: j.Hostname, Version: j.Version, Status: j.Status}
		if j.LastHeartbeat != nil {
			agent.LastHeartbeat = *j.LastHeartbeat
		}
		st.Agents = append(st.Agents, agent)
	}
	return st
}

// serveLocal starts the Unix socket for local tools in the data directory and
// returns the function that stops it. The socket is readable by the hub's
// user only; when it cannot be made the hub runs without it.
func (h *Hub) serveLocal(addr string) (stop func()) {
	path := localstatus.SocketPath(h.cfg.DataDir)
	if len(path) > maxSocketPath {
		h.log.Warn("the data directory path is too long for the local status socket; `regulus status` will not see live data", "path", path)
		return func() {}
	}
	_ = os.Remove(path) // left behind by a hub that was killed
	ln, err := net.Listen("unix", path)
	if err != nil {
		h.log.Warn("cannot open the local status socket", "path", path, "err", err)
		return func() {}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		h.log.Warn("cannot restrict the local status socket", "path", path, "err", err)
		_ = ln.Close()
		_ = os.Remove(path)
		return func() {}
	}
	srv := &http.Server{
		Handler:           localstatus.Handler(func() localstatus.Status { return h.localStatus(addr) }),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = os.Remove(path)
	}
}
