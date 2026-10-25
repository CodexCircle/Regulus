package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

type fakeSource struct {
	agents     []AgentInfo
	staleAfter time.Duration
	metrics    map[string][]MetricsSample
}

func (f fakeSource) Agents() []AgentInfo       { return f.agents }
func (f fakeSource) StaleAfter() time.Duration { return f.staleAfter }
func (f fakeSource) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte)
	return ch, func() {}
}
func (f fakeSource) Metrics(id string) ([]MetricsSample, bool) {
	samples, ok := f.metrics[id]
	return samples, ok
}

func getAgents(t *testing.T, src AgentSource) []agentJSON {
	t.Helper()
	rec := get(t, NewHandler(Deps{UI: fstest.MapFS{}, Agents: src}), "/api/agents")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var agents []agentJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return agents
}

func TestAgentsEndpointEmptyIsAnArray(t *testing.T) {
	for name, src := range map[string]AgentSource{"nil source": nil, "no agents": fakeSource{}} {
		rec := get(t, NewHandler(Deps{UI: fstest.MapFS{}, Agents: src}), "/api/agents")
		if got := rec.Body.String(); got != "[]\n" {
			t.Errorf("%s: body = %q, want %q", name, got, "[]\n")
		}
	}
}

func TestAgentsEndpointFields(t *testing.T) {
	now := time.Now()
	src := fakeSource{staleAfter: time.Minute, agents: []AgentInfo{{
		ID:      "a1",
		Version: "1.2.3",
		System: &agentv1.SystemInfo{
			Hostname: "srv", Os: "linux", OsVersion: "Ubuntu 24.04", Kernel: "6.8", Arch: "amd64",
			CpuCount: 4, MemoryTotalBytes: 8 << 30,
		},
		ConnectedAt:   now.Add(-time.Hour),
		LastHeartbeat: now.Add(-time.Second),
		LastMetrics:   &agentv1.MetricsReport{CollectedAt: timestamppb.New(now), UptimeSeconds: 42},
	}}}

	agents := getAgents(t, src)
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.ID != "a1" || a.Version != "1.2.3" || a.Status != statusConnected {
		t.Errorf("unexpected agent: %+v", a)
	}
	if a.Hostname != "srv" || a.OS != "linux" || a.OSVersion != "Ubuntu 24.04" || a.Arch != "amd64" ||
		a.CPUCount != 4 || a.MemoryTotalBytes != 8<<30 {
		t.Errorf("system info not mapped: %+v", a)
	}
	if a.LastHeartbeat == nil || a.MetricsAt == nil || a.UptimeSeconds == nil || *a.UptimeSeconds != 42 {
		t.Errorf("heartbeat and metrics not mapped: %+v", a)
	}
}

func TestAgentsEndpointStatus(t *testing.T) {
	now := time.Now()
	src := fakeSource{staleAfter: time.Minute, agents: []AgentInfo{
		{ID: "fresh", ConnectedAt: now.Add(-time.Hour), LastHeartbeat: now.Add(-time.Second)},
		{ID: "silent", ConnectedAt: now.Add(-time.Hour), LastHeartbeat: now.Add(-10 * time.Minute)},
		{ID: "new", ConnectedAt: now.Add(-time.Second)},
		{ID: "never", ConnectedAt: now.Add(-10 * time.Minute)},
	}}
	want := map[string]string{"fresh": statusConnected, "silent": statusStale, "new": statusConnected, "never": statusStale}
	for _, a := range getAgents(t, src) {
		if a.Status != want[a.ID] {
			t.Errorf("%s: status = %q, want %q", a.ID, a.Status, want[a.ID])
		}
		if a.ID == "never" || a.ID == "new" {
			if a.LastHeartbeat != nil || a.MetricsAt != nil || a.UptimeSeconds != nil {
				t.Errorf("%s: expected null heartbeat and metrics: %+v", a.ID, a)
			}
		}
	}
}

func TestAgentsEndpointListsBuiltinAgent(t *testing.T) {
	h := startHubWithConfig(t, config.Default())
	waitFor(t, "built-in agent heartbeat", func() bool {
		agents := h.Agents()
		return len(agents) == 1 && !agents[0].LastHeartbeat.IsZero() && agents[0].LastMetrics != nil
	})

	agents := getAgents(t, h)
	if len(agents) != 1 || agents[0].ID != BuiltinAgentID || agents[0].Status != statusConnected {
		t.Fatalf("unexpected agents: %+v", agents)
	}
	if agents[0].LastHeartbeat == nil || agents[0].UptimeSeconds == nil {
		t.Fatalf("missing heartbeat or uptime: %+v", agents[0])
	}
}

func TestAPIRequiresLogin(t *testing.T) {
	h := startHubWithConfig(t, config.Default())
	handler := NewHandler(Deps{UI: fstest.MapFS{"index.html": {Data: []byte("<div id=\"root\"></div>")}}, Agents: h, Layouts: h, Auth: h.auth})

	do := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	for _, path := range []string{"/api/agents", "/api/agents/" + BuiltinAgentID + "/metrics", "/api/nothing"} {
		if rec := do("GET", path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a login: status = %d, want 401", path, rec.Code)
		}
	}
	// The dashboard files and the health check stay public.
	for _, path := range []string{"/", "/healthz", "/api/auth/me"} {
		if rec := do("GET", path, "", nil); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}

	body := `{"username":"admin","password":"correct horse battery","setup_token":"` + h.auth.SetupToken() + `"}`
	rec := do("POST", "/api/auth/setup", body, nil)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("setup: status = %d, body = %s", rec.Code, rec.Body)
	}
	session := rec.Result().Cookies()[0]

	waitFor(t, "built-in agent", func() bool { return len(h.Agents()) == 1 })
	if rec := do("GET", "/api/agents", "", session); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agents with a login: status = %d", rec.Code)
	}
	if rec := do("GET", "/api/agents/"+BuiltinAgentID+"/metrics", "", session); rec.Code != http.StatusOK {
		t.Fatalf("GET metrics with a login: status = %d", rec.Code)
	}
}
