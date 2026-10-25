package hub

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

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
