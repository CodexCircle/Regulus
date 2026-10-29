package hub

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/config"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/update"
)

// startRolloutHub runs a hub (version 9.9.9) that has the release 9.9.9 on its
// release server and an agent listener.
func startRolloutHub(t *testing.T, mutate ...func(*config.Config)) (*Hub, releaseServer) {
	t.Helper()
	rs := newReleaseServer(t, "9.9.9")
	cfg := config.Default()
	cfg.BuiltinAgent = false
	cfg.AgentListen = "127.0.0.1:0"
	cfg.DataDir = t.TempDir()
	for _, m := range mutate {
		m(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h, err := New(ctx, cfg, quiet(), fstest.MapFS{}, WithVersion("9.9.9"), WithAgentIntervals(50*time.Millisecond, 50*time.Millisecond),
		WithUpdateSource(rs.Source), WithUpdateVerifier(acceptAll{}))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve returned %v", err)
		}
		_ = h.Close()
	})
	waitFor(t, "the agent listener", func() bool { return h.AgentAddr() != "" })
	h.rollout.returnTimeout = 5 * time.Second
	return h, rs
}

// runAgentUpdating is an agent that may update itself into dir.
func runAgentUpdating(t *testing.T, h *Hub, creds *agent.Credentials, version, dir string) (stop func()) {
	t.Helper()
	conn, err := creds.Dial(h.AgentAddr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = agent.Run(ctx, conn, agent.Options{
			Version: version, ID: creds.AgentID, Log: quiet(), UpdateDir: dir, Verifier: acceptAll{},
			MinBackoff: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		})
		_ = conn.Close()
	}()
	stop = func() { cancel(); <-done }
	t.Cleanup(stop)
	return stop
}

func decodeRollout(t *testing.T, rec interface {
	Result() *http.Response
}) RolloutStatus {
	t.Helper()
	var st RolloutStatus
	body, _ := io.ReadAll(rec.Result().Body)
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return st
}

// waitInstalling waits until the agent has answered the hub's update command.
// The installer restarts the agent only after that, so a test that restarts the
// agent earlier would race with the answer and see "disconnected before
// answering" instead of the outcome it means to test.
func waitInstalling(t *testing.T, admin *api) {
	t.Helper()
	waitFor(t, "the agent to be installing", func() bool {
		st := decodeRollout(t, admin.do("GET", "/api/updates/rollout", nil))
		return len(st.Agents) == 1 && st.Agents[0].Status == AgentInstalling
	})
}

func waitRollout(t *testing.T, admin *api, state string) RolloutStatus {
	t.Helper()
	var st RolloutStatus
	waitFor(t, "the rollout to be "+state, func() bool {
		st = decodeRollout(t, admin.do("GET", "/api/updates/rollout", nil))
		return st.State == state
	})
	return st
}

// The agent is behind, takes the release from the hub over its connection,
// leaves a request for the installer, and comes back with the new version.
func TestRolloutUpdatesAnAgentBehind(t *testing.T) {
	h, rs := startRolloutHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	creds := enrollNew(t, h, "web1")
	dir := t.TempDir()
	stop := runAgentUpdating(t, h, creds, "9.8.1", dir)
	waitFor(t, "the agent", func() bool { return len(h.Agents()) == 1 })

	st := decodeRollout(t, admin.do("GET", "/api/updates/rollout", nil))
	if st.Target != "9.9.9" || st.State != RolloutIdle || len(st.Candidates) != 1 || st.Candidates[0].From != "9.8.1" {
		t.Fatalf("status = %+v", st)
	}

	rec := admin.do("POST", "/api/updates/rollout", map[string]any{"wave_size": 1})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}

	// The agent hands the release to the installer.
	applyDir := update.ApplyDir(dir)
	waitFor(t, "the request for the installer", func() bool { _, ok := update.ReadRequest(applyDir); return ok })
	req, _ := update.ReadRequest(applyDir)
	if req.Version != "9.9.9" || req.RequestedBy != "hub" {
		t.Errorf("request = %+v", req)
	}
	archive, err := os.ReadFile(filepath.Join(dir, "updates", "9.9.9", rs.archive))
	if err != nil || !strings.HasPrefix(string(archive), "archive of 9.9.9") {
		t.Errorf("archive in the agent = %q, %v", archive, err)
	}
	waitInstalling(t, admin)
	if st := decodeRollout(t, admin.do("GET", "/api/updates/rollout", nil)); st.State != RolloutRunning {
		t.Errorf("while installing: %+v", st)
	}
	if rec := admin.do("POST", "/api/updates/rollout", map[string]any{}); rec.Code != http.StatusConflict {
		t.Errorf("a second rollout: %d", rec.Code)
	}

	// The installer replaces the binary and restarts the agent.
	_ = os.Remove(filepath.Join(applyDir, "request.json"))
	stop()
	runAgentUpdating(t, h, creds, "9.9.9", dir)

	st = waitRollout(t, admin, RolloutDone)
	if len(st.Agents) != 1 || st.Agents[0].Status != AgentUpdated || st.StartedBy != "admin" || st.FinishedAt == nil {
		t.Errorf("after: %+v", st)
	}
	if got := len(st.Candidates); got != 0 {
		t.Errorf("candidates after the update: %d", got)
	}
	entries, _ := h.auth.Audit(context.Background(), 50)
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Event] = true
	}
	for _, ev := range []string{EventRolloutStarted, EventAgentUpdated, EventRolloutFinished} {
		if !seen[ev] {
			t.Errorf("audit lacks %s", ev)
		}
	}
	// The connected marker lets the installer judge the restarted agent.
	if c, ok := update.ReadConnected(dir); !ok || c.Version != "9.9.9" {
		t.Errorf("connected = %+v, %v", c, ok)
	}
}

func TestRolloutStopsAtTheFirstFailure(t *testing.T) {
	h, _ := startRolloutHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	// Neither agent is set up to update itself.
	for _, name := range []string{"a", "b"} {
		runAgentVersion(t, h, enrollNew(t, h, name), "9.8.1")
	}
	waitFor(t, "the agents", func() bool { return len(h.Agents()) == 2 })

	if rec := admin.do("POST", "/api/updates/rollout", map[string]any{"wave_size": 1}); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	st := waitRollout(t, admin, RolloutFailed)
	counts := map[string]int{}
	for _, a := range st.Agents {
		counts[a.Status]++
		if a.Status == AgentFailed && !strings.Contains(a.Message, "not set up to update itself") {
			t.Errorf("message = %q", a.Message)
		}
	}
	if counts[AgentFailed] != 1 || counts[AgentSkipped] != 1 {
		t.Errorf("agents = %+v", st.Agents)
	}
}

func TestRolloutNoticesAnAgentThatCameBackRolledBack(t *testing.T) {
	h, _ := startRolloutHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	creds := enrollNew(t, h, "web1")
	dir := t.TempDir()
	stop := runAgentUpdating(t, h, creds, "9.8.1", dir)
	waitFor(t, "the agent", func() bool { return len(h.Agents()) == 1 })

	admin.do("POST", "/api/updates/rollout", map[string]any{})
	waitFor(t, "the request", func() bool { _, ok := update.ReadRequest(update.ApplyDir(dir)); return ok })
	waitInstalling(t, admin)
	// The installer rolled back: the old version is running again.
	_ = os.Remove(filepath.Join(update.ApplyDir(dir), "request.json"))
	stop()
	runAgentUpdating(t, h, creds, "9.8.1", dir)

	st := waitRollout(t, admin, RolloutFailed)
	if a := st.Agents[0]; a.Status != AgentFailed || !strings.Contains(a.Message, "rolled back") {
		t.Errorf("agent = %+v", a)
	}
}

func TestRolloutGivesUpOnAnAgentThatDoesNotReturn(t *testing.T) {
	h, _ := startRolloutHub(t)
	h.rollout.returnTimeout = 300 * time.Millisecond
	admin := newAPI(t, h, auth.RoleAdmin)
	creds := enrollNew(t, h, "web1")
	runAgentUpdating(t, h, creds, "9.8.1", t.TempDir())
	waitFor(t, "the agent", func() bool { return len(h.Agents()) == 1 })

	admin.do("POST", "/api/updates/rollout", map[string]any{})
	st := waitRollout(t, admin, RolloutFailed)
	if a := st.Agents[0]; !strings.Contains(a.Message, "did not connect again") {
		t.Errorf("agent = %+v", a)
	}
}

func TestRolloutRefusals(t *testing.T) {
	h, _ := startRolloutHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	if rec := admin.do("POST", "/api/updates/rollout", map[string]any{}); rec.Code != http.StatusConflict {
		t.Errorf("no agent behind: %d %s", rec.Code, rec.Body)
	}
	for name, body := range map[string]map[string]any{
		"negative wave": {"wave_size": -1},
		"huge wave":     {"wave_size": 1000},
		"unknown field": {"extra": 1},
	} {
		if rec := admin.do("POST", "/api/updates/rollout", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	op := newAPI(t, startRemoteHub(t), auth.RoleOperator)
	if rec := op.do("POST", "/api/updates/rollout", map[string]any{}); rec.Code != http.StatusForbidden {
		t.Errorf("operator: %d", rec.Code)
	}
	if rec := op.do("GET", "/api/updates/rollout", nil); rec.Code != http.StatusForbidden {
		t.Errorf("operator status: %d", rec.Code)
	}
	if rec := admin.do("POST", "/api/updates/rollout/stop", struct{}{}); rec.Code != http.StatusOK {
		t.Errorf("stop with nothing running: %d", rec.Code)
	}
}

func TestRolloutNeedsAReleaseBuild(t *testing.T) {
	h := newUpdateHub(t, "dev", staticSource{})
	if _, err := h.StartRollout(t.Context(), "admin", RolloutOptions{}); err != ErrNotRelease {
		t.Errorf("error = %v", err)
	}
}

// An agent gets only the release the hub runs, its own build, and only while
// connected: the cache is not a file server.
func TestFetchUpdateServesOnlyWhatItShould(t *testing.T) {
	h, _ := startRolloutHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	creds := enrollNew(t, h, "web1")
	runAgentVersion(t, h, creds, "9.8.1")
	waitFor(t, "the agent", func() bool { return len(h.Agents()) == 1 })
	// Put the release in the cache.
	if rec := admin.do("POST", "/api/updates/rollout", map[string]any{}); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	waitRollout(t, admin, RolloutFailed) // the agent cannot update itself, which is fine here

	conn, err := creds.Dial(h.AgentAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	client := agentv1.NewAgentServiceClient(conn)
	fetch := func(version string, file agentv1.UpdateFile) (int, error) {
		stream, err := client.FetchUpdate(t.Context(), &agentv1.FetchUpdateRequest{Version: version, File: file})
		if err != nil {
			return 0, err
		}
		n := 0
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				return n, nil
			}
			if err != nil {
				return n, err
			}
			n += len(chunk.GetData())
		}
	}
	for _, f := range []agentv1.UpdateFile{agentv1.UpdateFile_UPDATE_FILE_ARCHIVE, agentv1.UpdateFile_UPDATE_FILE_CHECKSUMS, agentv1.UpdateFile_UPDATE_FILE_BUNDLE} {
		if n, err := fetch("9.9.9", f); err != nil || n == 0 {
			t.Errorf("file %v: %d bytes, %v", f, n, err)
		}
	}
	for name, tc := range map[string]struct {
		version string
		file    agentv1.UpdateFile
		code    codes.Code
	}{
		"another version": {"1.0.0", agentv1.UpdateFile_UPDATE_FILE_ARCHIVE, codes.NotFound},
		"a path":          {"../../etc", agentv1.UpdateFile_UPDATE_FILE_ARCHIVE, codes.NotFound},
		"no file":         {"9.9.9", agentv1.UpdateFile_UPDATE_FILE_UNSPECIFIED, codes.InvalidArgument},
		"not the hub's":   {"9.8.1", agentv1.UpdateFile_UPDATE_FILE_ARCHIVE, codes.NotFound},
	} {
		if _, err := fetch(tc.version, tc.file); status.Code(err) != tc.code {
			t.Errorf("%s: error = %v, want %v", name, err, tc.code)
		}
	}
}

func TestAutomaticModeUpdatesTheServersInsideTheWindowOnly(t *testing.T) {
	h, _ := startRolloutHub(t, func(c *config.Config) { c.Updates.Apply = true })
	creds := enrollNew(t, h, "web1")
	runAgentVersion(t, h, creds, "9.8.1")
	waitFor(t, "the agent", func() bool { return len(h.Agents()) == 1 })

	now := time.Now()
	around := func(from, to time.Duration) update.Settings {
		return update.Settings{Mode: update.ModeAutomatic, Channel: update.Stable,
			WindowStart: now.Add(from).Format("15:04"), WindowEnd: now.Add(to).Format("15:04")}
	}
	if _, err := h.updates.SetSettings(t.Context(), "admin", around(3*time.Hour, 5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	h.autoRollout(t.Context())
	if st, _ := h.RolloutStatus(t.Context()); st.State != RolloutIdle {
		t.Fatalf("a rollout started outside the window: %+v", st)
	}

	if _, err := h.updates.SetSettings(t.Context(), "admin", around(-time.Hour, time.Hour)); err != nil {
		t.Fatal(err)
	}
	h.autoRollout(t.Context())
	st, _ := h.RolloutStatus(t.Context())
	if st.State == RolloutIdle || st.StartedBy != update.SystemActor {
		t.Fatalf("no rollout inside the window: %+v", st)
	}
	// The agent cannot update itself, so the rollout fails, and the automatic
	// mode does not start it again by itself.
	waitFor(t, "the rollout to fail", func() bool {
		st, _ := h.RolloutStatus(t.Context())
		return st.State == RolloutFailed
	})
	h.autoRollout(t.Context())
	if st, _ := h.RolloutStatus(t.Context()); st.State != RolloutFailed || len(st.Agents) != 1 {
		t.Errorf("the failed rollout was started again: %+v", st)
	}
}
