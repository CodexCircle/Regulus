package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/CodexCircle/Regulus/internal/fleet"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// AgentSource provides the agent data served by the HTTP API. *Hub implements
// it.
type AgentSource interface {
	Agents() []AgentInfo
	// StaleAfter is how long an agent may stay silent before it is reported as
	// stale.
	StaleAfter() time.Duration
	// Metrics returns the recent metrics of an agent, oldest first; known is
	// false for an agent the hub never saw.
	Metrics(agentID string) (samples []MetricsSample, known bool)
	// Subscribe returns a channel of JSON events (see events.go) and a function
	// that ends the subscription. The channel is closed if the subscriber falls
	// too far behind.
	Subscribe() (events <-chan []byte, cancel func())
}

// Agent statuses reported by GET /api/agents. The last two only appear with
// ?all=true, for enrolled agents that are not connected.
const (
	statusConnected = "connected"
	statusStale     = "stale"
	statusOffline   = "offline"
	statusRevoked   = "revoked"
	// statusIncompatible: an enrolled agent the hub refused because its
	// version cannot work with this hub; see incompatible_reason.
	statusIncompatible = "incompatible"
)

// agentJSON is the JSON form of an agent in GET /api/agents.
type agentJSON struct {
	ID               string     `json:"id"`
	Name             string     `json:"name,omitempty"`
	Version          string     `json:"version"`
	Status           string     `json:"status"`
	Hostname         string     `json:"hostname,omitempty"`
	OS               string     `json:"os,omitempty"`
	OSVersion        string     `json:"os_version,omitempty"`
	Kernel           string     `json:"kernel,omitempty"`
	Arch             string     `json:"arch,omitempty"`
	CPUCount         uint32     `json:"cpu_count,omitempty"`
	MemoryTotalBytes uint64     `json:"memory_total_bytes,omitempty"`
	ConnectedAt      *time.Time `json:"connected_at"`
	LastSeenAt       *time.Time `json:"last_seen_at,omitempty"`
	LastHeartbeat    *time.Time `json:"last_heartbeat"`
	MetricsAt        *time.Time `json:"metrics_at"`
	UptimeSeconds    *uint64    `json:"uptime_seconds"`
	// Compatibility says how the agent's release relates to the hub's:
	// current, behind, ahead or unknown (see update.Compat).
	Compatibility   string `json:"compatibility,omitempty"`
	ProtocolVersion uint32 `json:"protocol_version,omitempty"`
	// IncompatibleReason is why the hub refused the agent, with the status
	// incompatible.
	IncompatibleReason string `json:"incompatible_reason,omitempty"`
}

func toAgentJSON(a AgentInfo, now time.Time, staleAfter time.Duration) agentJSON {
	connected := a.ConnectedAt.UTC()
	out := agentJSON{
		ID:          a.ID,
		Version:     a.Version,
		Status:      statusConnected,
		ConnectedAt: &connected,

		Compatibility:   string(a.Compat),
		ProtocolVersion: a.ProtocolVersion,
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

// registrySource is implemented by sources that know the enrolled agents,
// including those that are not connected.
type registrySource interface {
	Registry(ctx context.Context) ([]fleet.Agent, error)
}

// agentsHandler serves the agents as a JSON array, ordered by id: the
// connected ones, or with ?all=true every enrolled agent too, with the status
// "offline" or "revoked" for those that are not connected.
func agentsHandler(src AgentSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agents := []agentJSON{}
		if src != nil {
			names := map[string]fleet.Agent{}
			var enrolled []fleet.Agent
			if reg, ok := src.(registrySource); ok {
				var err error
				if enrolled, err = reg.Registry(r.Context()); err != nil {
					writeJSONError(w, http.StatusInternalServerError, "internal error")
					return
				}
			}
			for _, e := range enrolled {
				names[e.ID] = e
			}

			now := time.Now()
			staleAfter := src.StaleAfter()
			seen := map[string]bool{}
			for _, a := range src.Agents() {
				j := toAgentJSON(a, now, staleAfter)
				j.Name = names[a.ID].Name
				agents = append(agents, j)
				seen[a.ID] = true
			}
			if r.URL.Query().Get("all") == "true" {
				for _, e := range enrolled {
					if !seen[e.ID] {
						agents = append(agents, offlineJSON(e, src))
					}
				}
				slices.SortFunc(agents, func(a, b agentJSON) int { return strings.Compare(a.ID, b.ID) })
			}
		}
		writeJSONBody(w, http.StatusOK, agents)
	}
}

// rejectionSource is implemented by sources that remember why they refused an
// enrolled agent.
type rejectionSource interface {
	Rejection(id string) (Rejection, bool)
}

// offlineJSON is the JSON form of an enrolled agent that is not connected.
func offlineJSON(e fleet.Agent, src AgentSource) agentJSON {
	out := agentJSON{ID: e.ID, Name: e.Name, Version: e.Version, Status: statusOffline, Hostname: e.Hostname, OSVersion: e.OSVersion}
	if e.Revoked {
		out.Status = statusRevoked
	} else if rs, ok := src.(rejectionSource); ok {
		if r, ok := rs.Rejection(e.ID); ok {
			out.Status = statusIncompatible
			out.IncompatibleReason = r.Message
			if r.Version != "" {
				out.Version = r.Version
			}
		}
	}
	if !e.LastSeen.IsZero() {
		t := e.LastSeen.UTC()
		out.LastSeenAt = &t
	}
	return out
}

// metricsJSON is the JSON form of GET /api/agents/{id}/metrics.
type metricsJSON struct {
	Agent  string      `json:"agent"`
	Points []pointJSON `json:"points"`
}

// pointJSON is one metrics report. Network rates are computed by the hub from
// the counters of two consecutive reports and are null for the first one and
// after a counter reset.
type pointJSON struct {
	At                   time.Time     `json:"at"`
	CPUPercent           float64       `json:"cpu_percent"`
	Load1                float64       `json:"load_1"`
	Load5                float64       `json:"load_5"`
	Load15               float64       `json:"load_15"`
	MemoryTotalBytes     uint64        `json:"memory_total_bytes"`
	MemoryUsedBytes      uint64        `json:"memory_used_bytes"`
	MemoryAvailableBytes uint64        `json:"memory_available_bytes"`
	SwapTotalBytes       uint64        `json:"swap_total_bytes"`
	SwapUsedBytes        uint64        `json:"swap_used_bytes"`
	UptimeSeconds        uint64        `json:"uptime_seconds"`
	Disks                []diskJSON    `json:"disks"`
	Networks             []networkJSON `json:"networks"`
}

type diskJSON struct {
	MountPoint string `json:"mount_point"`
	Filesystem string `json:"filesystem"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

type networkJSON struct {
	Interface     string   `json:"interface"`
	RxBytes       uint64   `json:"rx_bytes"`
	TxBytes       uint64   `json:"tx_bytes"`
	RxBytesPerSec *float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec *float64 `json:"tx_bytes_per_sec"`
}

func toPoints(samples []MetricsSample) []pointJSON {
	points := make([]pointJSON, 0, len(samples))
	var prev *MetricsSample
	for i := range samples {
		points = append(points, toPoint(samples[i], prev))
		prev = &samples[i]
	}
	return points
}

func toPoint(s MetricsSample, prev *MetricsSample) pointJSON {
	r := s.Report
	p := pointJSON{
		At:                   s.At.UTC(),
		CPUPercent:           r.GetCpu().GetUsagePercent(),
		Load1:                r.GetCpu().GetLoad_1(),
		Load5:                r.GetCpu().GetLoad_5(),
		Load15:               r.GetCpu().GetLoad_15(),
		MemoryTotalBytes:     r.GetMemory().GetTotalBytes(),
		MemoryUsedBytes:      r.GetMemory().GetUsedBytes(),
		MemoryAvailableBytes: r.GetMemory().GetAvailableBytes(),
		SwapTotalBytes:       r.GetMemory().GetSwapTotalBytes(),
		SwapUsedBytes:        r.GetMemory().GetSwapUsedBytes(),
		UptimeSeconds:        r.GetUptimeSeconds(),
		Disks:                []diskJSON{},
		Networks:             []networkJSON{},
	}
	for _, d := range r.GetDisks() {
		p.Disks = append(p.Disks, diskJSON{d.GetMountPoint(), d.GetFilesystem(), d.GetTotalBytes(), d.GetUsedBytes()})
	}

	var before map[string]*agentv1.NetworkMetrics
	var seconds float64
	if prev != nil {
		seconds = s.At.Sub(prev.At).Seconds()
		before = map[string]*agentv1.NetworkMetrics{}
		for _, n := range prev.Report.GetNetworks() {
			before[n.GetInterface()] = n
		}
	}
	for _, n := range r.GetNetworks() {
		out := networkJSON{Interface: n.GetInterface(), RxBytes: n.GetRxBytes(), TxBytes: n.GetTxBytes()}
		if old := before[n.GetInterface()]; old != nil && seconds > 0 &&
			n.GetRxBytes() >= old.GetRxBytes() && n.GetTxBytes() >= old.GetTxBytes() {
			rx := float64(n.GetRxBytes()-old.GetRxBytes()) / seconds
			tx := float64(n.GetTxBytes()-old.GetTxBytes()) / seconds
			out.RxBytesPerSec, out.TxBytesPerSec = &rx, &tx
		}
		p.Networks = append(p.Networks, out)
	}
	return p
}

// metricsHandler serves the recent metrics of an agent, oldest first. The
// optional query parameter since (RFC 3339) returns only points received after
// it, which lets a client poll cheaply.
func metricsHandler(src AgentSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var since time.Time
		if v := r.URL.Query().Get("since"); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				http.Error(w, "since must be an RFC 3339 timestamp", http.StatusBadRequest)
				return
			}
			since = t
		}
		if src == nil {
			http.Error(w, "unknown agent", http.StatusNotFound)
			return
		}
		samples, known := src.Metrics(id)
		if !known {
			http.Error(w, "unknown agent", http.StatusNotFound)
			return
		}
		// Rates need the previous report, so they are computed before filtering.
		points := toPoints(samples)
		if !since.IsZero() {
			points = slices.DeleteFunc(points, func(p pointJSON) bool { return !p.At.After(since) })
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(metricsJSON{Agent: id, Points: points})
	}
}

// agentGetHandler serves one agent: a connected one, or an enrolled one that
// is offline or revoked.
func agentGetHandler(src AgentSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if src != nil {
			var enrolled []fleet.Agent
			if reg, ok := src.(registrySource); ok {
				var err error
				if enrolled, err = reg.Registry(r.Context()); err != nil {
					writeJSONError(w, http.StatusInternalServerError, "internal error")
					return
				}
			}
			now, staleAfter := time.Now(), src.StaleAfter()
			for _, a := range src.Agents() {
				if a.ID == id {
					j := toAgentJSON(a, now, staleAfter)
					for _, e := range enrolled {
						if e.ID == id {
							j.Name = e.Name
						}
					}
					writeJSONBody(w, http.StatusOK, j)
					return
				}
			}
			for _, e := range enrolled {
				if e.ID == id {
					writeJSONBody(w, http.StatusOK, offlineJSON(e, src))
					return
				}
			}
		}
		writeJSONError(w, http.StatusNotFound, "unknown agent")
	}
}

func writeJSONBody(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSONBody(w, status, map[string]string{"error": msg})
}
