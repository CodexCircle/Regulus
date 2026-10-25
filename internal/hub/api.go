package hub

import (
	"encoding/json"
	"net/http"
	"time"
)

// AgentSource provides the agent data served by the HTTP API. *Hub implements
// it.
type AgentSource interface {
	Agents() []AgentInfo
	// StaleAfter is how long an agent may stay silent before it is reported as
	// stale.
	StaleAfter() time.Duration
}

// Agent statuses reported by GET /api/agents.
const (
	statusConnected = "connected"
	statusStale     = "stale"
)

// agentJSON is the JSON form of an agent in GET /api/agents.
type agentJSON struct {
	ID               string     `json:"id"`
	Version          string     `json:"version"`
	Status           string     `json:"status"`
	Hostname         string     `json:"hostname,omitempty"`
	OS               string     `json:"os,omitempty"`
	OSVersion        string     `json:"os_version,omitempty"`
	Kernel           string     `json:"kernel,omitempty"`
	Arch             string     `json:"arch,omitempty"`
	CPUCount         uint32     `json:"cpu_count,omitempty"`
	MemoryTotalBytes uint64     `json:"memory_total_bytes,omitempty"`
	ConnectedAt      time.Time  `json:"connected_at"`
	LastHeartbeat    *time.Time `json:"last_heartbeat"`
	MetricsAt        *time.Time `json:"metrics_at"`
	UptimeSeconds    *uint64    `json:"uptime_seconds"`
}

func toAgentJSON(a AgentInfo, now time.Time, staleAfter time.Duration) agentJSON {
	out := agentJSON{
		ID:          a.ID,
		Version:     a.Version,
		Status:      statusConnected,
		ConnectedAt: a.ConnectedAt.UTC(),
	}
	if s := a.System; s != nil {
		out.Hostname = s.Hostname
		out.OS = s.Os
		out.OSVersion = s.OsVersion
		out.Kernel = s.Kernel
		out.Arch = s.Arch
		out.CPUCount = s.CpuCount
		out.MemoryTotalBytes = s.MemoryTotalBytes
	}

	lastSeen := a.ConnectedAt
	if !a.LastHeartbeat.IsZero() {
		t := a.LastHeartbeat.UTC()
		out.LastHeartbeat = &t
		lastSeen = a.LastHeartbeat
	}
	if now.Sub(lastSeen) > staleAfter {
		out.Status = statusStale
	}

	if m := a.LastMetrics; m != nil {
		if m.CollectedAt != nil {
			t := m.CollectedAt.AsTime().UTC()
			out.MetricsAt = &t
		}
		uptime := m.UptimeSeconds
		out.UptimeSeconds = &uptime
	}
	return out
}

// agentsHandler serves the connected agents as a JSON array, ordered by id.
// It is read-only; authentication comes with the API work.
func agentsHandler(src AgentSource) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		agents := []agentJSON{}
		if src != nil {
			now := time.Now()
			staleAfter := src.StaleAfter()
			for _, a := range src.Agents() {
				agents = append(agents, toAgentJSON(a, now, staleAfter))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(agents)
	}
}
