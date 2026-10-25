// Package localstatus is the small channel between a running hub and the
// tools on the same machine (the console and `regulus status`): a Unix socket
// in the data directory, readable only by the user that runs the hub. It
// carries no login; the file permissions are the proof of who may ask.
package localstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

// SocketName is the file name of the socket inside the data directory.
const SocketName = "regulus.sock"

// SocketPath is where the hub of dataDir listens for local tools.
func SocketPath(dataDir string) string {
	return filepath.Join(dataDir, SocketName)
}

// Status is what a running hub reports about itself.
type Status struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	// Addr is where the dashboard is bound; AgentAddr where remote agents
	// connect, or "" while that is off.
	Addr         string `json:"addr"`
	AgentAddr    string `json:"agent_addr,omitempty"`
	BuiltinAgent bool   `json:"builtin_agent"`
	// SetupToken is the one-time token to create the first admin; it is
	// empty once the admin exists.
	SetupToken string  `json:"setup_token,omitempty"`
	Agents     []Agent `json:"agents"`
}

// Agent is a connected agent.
type Agent struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname,omitempty"`
	Version       string    `json:"version,omitempty"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitzero"`
}

// Handler serves the status as JSON at /status.
func Handler(get func() Status) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(get())
	})
	return mux
}

// Fetch asks the hub of dataDir for its status. The error is not nil when no
// hub is listening there.
func Fetch(ctx context.Context, dataDir string) (Status, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", SocketPath(dataDir))
			},
		},
		Timeout: 3 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hub/status", nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("the hub answered %s", resp.Status)
	}
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return Status{}, err
	}
	return st, nil
}
