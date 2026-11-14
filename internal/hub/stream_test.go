package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/CodexCircle/Regulus/internal/auth"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

func TestBrokerDeliversAndDropsSlowSubscribers(t *testing.T) {
	b := newBroker()
	fast, cancelFast := b.subscribe()
	defer cancelFast()
	slow, _ := b.subscribe()

	// The fast subscriber keeps up, the slow one never reads.
	for i := range subscriberBuffer + 1 {
		b.publish(map[string]int{"n": i})
		<-fast
	}
	got := 0
	for range slow {
		got++
	}
	if got != subscriberBuffer {
		t.Fatalf("slow subscriber got %d events before being dropped, want %d", got, subscriberBuffer)
	}

	b.publish("still works")
	if msg := <-fast; string(msg) != `"still works"` {
		t.Fatalf("fast subscriber got %s", msg)
	}
	cancelFast()
	cancelFast() // cancelling twice is harmless
	if _, ok := <-fast; ok {
		t.Fatal("a cancelled subscription must be closed")
	}
	b.publish("nobody listens") // must not panic
}

// dialStream opens the stream of a hub served without a login.
func dialStream(t *testing.T, h *Hub) (*websocket.Conn, context.Context) {
	t.Helper()
	srv := httptest.NewServer(NewHandler(Deps{UI: fstest.MapFS{}, Agents: h}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn, ctx
}

// next reads messages until one has the wanted type and returns it decoded.
func next(t *testing.T, ctx context.Context, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", typ, err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("invalid message %q: %v", data, err)
		}
		if msg["type"] == typ {
			return msg
		}
	}
}

func TestStreamSendsSnapshotThenEvents(t *testing.T) {
	h := startHub(t)
	stream, _ := register(t, h, "a1")
	waitFor(t, "agent", func() bool { return len(h.Agents()) == 1 })

	conn, ctx := dialStream(t, h)
	snap := next(t, ctx, conn, "snapshot")
	agents, _ := snap["agents"].([]any)
	if len(agents) != 1 || agents[0].(map[string]any)["id"] != "a1" || snap["stale_after_seconds"].(float64) <= 0 {
		t.Fatalf("unexpected snapshot: %v", snap)
	}

	// Events from the agent.
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{SentAt: timestamppb.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if hb := next(t, ctx, conn, "agent.heartbeat"); hb["id"] != "a1" || hb["at"] == nil {
		t.Fatalf("unexpected heartbeat event: %v", hb)
	}
	report := &agentv1.MetricsReport{
		CollectedAt: timestamppb.Now(), UptimeSeconds: 77,
		Cpu: &agentv1.CpuMetrics{UsagePercent: 42},
	}
	if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Metrics{Metrics: report}}); err != nil {
		t.Fatal(err)
	}
	m := next(t, ctx, conn, "agent.metrics")
	point, _ := m["point"].(map[string]any)
	if m["id"] != "a1" || point["cpu_percent"] != 42.0 || point["uptime_seconds"] != 77.0 {
		t.Fatalf("unexpected metrics event: %v", m)
	}

	// A second agent connecting and leaving.
	other, _ := register(t, h, "a2")
	if c := next(t, ctx, conn, "agent.connected"); c["agent"].(map[string]any)["id"] != "a2" {
		t.Fatalf("unexpected connected event: %v", c)
	}
	_ = other.CloseSend()
	if g := next(t, ctx, conn, "agent.disconnected"); g["id"] != "a2" {
		t.Fatalf("unexpected disconnected event: %v", g)
	}
}

func TestReplacedConnectionIsNotReportedAsDisconnected(t *testing.T) {
	h := startHub(t)
	old, _ := register(t, h, "a1")
	events, cancel := h.Subscribe()
	defer cancel()

	_, _ = register(t, h, "a1") // the same agent reconnects
	if _, err := old.Recv(); err == nil {
		t.Fatal("the replaced stream should have been closed")
	}

	// The old stream has ended by now. The new one is silent, and the hub drops
	// it only after three heartbeat intervals (300 ms), so look before that.
	sawConnected := false
	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case msg := <-events:
			var e struct{ Type string }
			_ = json.Unmarshal(msg, &e)
			if e.Type == "agent.disconnected" {
				t.Fatalf("a replaced connection must not be reported as a disconnection: %s", msg)
			}
			sawConnected = sawConnected || e.Type == "agent.connected"
		case <-deadline:
			if !sawConnected {
				t.Fatal("no agent.connected event for the new connection")
			}
			return
		}
	}
}

func TestStreamRefusesOtherOriginsAndAnonymousClients(t *testing.T) {
	h := startHub(t)

	// Another site's page cannot open the stream.
	srv := httptest.NewServer(NewHandler(Deps{UI: fstest.MapFS{}, Agents: h}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/stream", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("expected the cross-origin upgrade to be refused")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin upgrade: response = %v, want 403", resp)
	}

	// Without a login the upgrade never happens.
	secured := httptest.NewServer(NewHandler(Deps{UI: fstest.MapFS{}, Agents: h, Auth: h.auth}))
	defer secured.Close()
	_, resp, err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(secured.URL, "http")+"/api/stream", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous upgrade: err = %v, response = %v, want 401", err, resp)
	}
}

// An open stream must end when its session does: logging out, disabling the
// account or taking the permission away only change the database, so the
// stream has to ask.
func TestStreamEndsWhenTheSessionEnds(t *testing.T) {
	old := sessionRecheck
	sessionRecheck = 50 * time.Millisecond
	t.Cleanup(func() { sessionRecheck = old })

	h := startHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, adminToken, _, err := h.auth.Setup(ctx, h.auth.SetupToken(), "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.auth.CreateUser(ctx, admin, "viewer1", "correct horse battery", auth.RoleViewer); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(Deps{UI: fstest.MapFS{}, Agents: h, Auth: h.auth}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/stream"

	open := func(token string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {auth.CookieName + "=" + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		next(t, ctx, conn, "snapshot")
		return conn
	}
	// closed waits for the server to end the stream and returns the close status.
	closed := func(conn *websocket.Conn) websocket.StatusCode {
		t.Helper()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return websocket.CloseStatus(err)
			}
		}
	}
	login := func(name string) string {
		t.Helper()
		_, token, _, err := h.auth.Login(ctx, "198.51.100."+name[len(name)-1:], name, "correct horse battery")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	// Logout.
	token := login("viewer1")
	conn := open(token)
	if err := h.auth.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if code := closed(conn); code != websocket.StatusPolicyViolation {
		t.Errorf("after logout the stream ended with %v, want a policy violation", code)
	}

	// Disabled account.
	conn = open(login("viewer1"))
	disabled := true
	if _, err := h.auth.UpdateUser(ctx, admin, "viewer1", auth.UserChange{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if code := closed(conn); code != websocket.StatusPolicyViolation {
		t.Errorf("after disabling the account the stream ended with %v, want a policy violation", code)
	}

	// A session that stays valid keeps its stream.
	conn = open(adminToken)
	readCtx, stop := context.WithTimeout(ctx, 400*time.Millisecond)
	defer stop()
	if _, _, err := conn.Read(readCtx); err == nil || websocket.CloseStatus(err) != -1 {
		t.Errorf("a valid session lost its stream: %v", err)
	}
}
