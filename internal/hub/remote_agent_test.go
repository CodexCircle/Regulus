package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/fleet"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/pki"
)

// startRemoteHub runs a hub with the agent listener on and the built-in agent
// off, and waits until the listener is bound.
func startRemoteHub(t *testing.T) *Hub {
	t.Helper()
	cfg := config.Default()
	cfg.BuiltinAgent = false
	cfg.AgentListen = "127.0.0.1:0"
	h := startHubWithConfig(t, cfg)
	waitFor(t, "the agent listener", func() bool { return h.AgentAddr() != "" })
	return h
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// runAgent runs a remote agent until the test ends or stop is called, and
// returns a channel with the result of agent.Run.
func runAgent(t *testing.T, h *Hub, creds *agent.Credentials) (stop func(), result <-chan error) {
	t.Helper()
	conn, err := creds.Dial(h.AgentAddr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() {
		res <- agent.Run(ctx, conn, agent.Options{
			Version: "9.9.9", ID: creds.AgentID, Log: quiet(),
			MinBackoff: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		})
		_ = conn.Close()
	}()
	t.Cleanup(cancel)
	return cancel, res
}

func enrollNew(t *testing.T, h *Hub, name string) *agent.Credentials {
	t.Helper()
	grant, err := h.CreateEnrollment(context.Background(), "admin", name)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := agent.Enroll(context.Background(), h.AgentAddr(), grant.CAFingerprint, grant.Token, "9.9.9", t.TempDir())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return creds
}

func TestRemoteAgentEnrollsConnectsAndIsRevoked(t *testing.T) {
	h := startRemoteHub(t)
	creds := enrollNew(t, h, "web1")
	if !strings.HasPrefix(creds.AgentID, "agent-") {
		t.Fatalf("agent id = %q", creds.AgentID)
	}

	stop, result := runAgent(t, h, creds)
	waitFor(t, "the remote agent to report", func() bool {
		for _, a := range h.Agents() {
			if a.ID == creds.AgentID && a.LastMetrics != nil && !a.LastHeartbeat.IsZero() {
				return true
			}
		}
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := h.SendCommand(ctx, creds.AgentID, &agentv1.Command{Kind: &agentv1.Command_Ping{Ping: &agentv1.Ping{}}})
	if err != nil || !res.GetSuccess() {
		t.Fatalf("ping: %v, %v", res, err)
	}

	// Revoking closes the connection, and the agent is not let back in.
	if err := h.RevokeAgent(ctx, "admin", creds.AgentID); err != nil {
		t.Fatal(err)
	}
	var rejected *agent.RejectedError
	select {
	case err := <-result:
		if !errors.As(err, &rejected) {
			t.Fatalf("Run returned %v, want a RejectedError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the revoked agent kept running")
	}
	waitFor(t, "the revoked agent to disappear", func() bool { return len(h.Agents()) == 0 })

	_, result = runAgent(t, h, creds)
	select {
	case err := <-result:
		if !errors.As(err, &rejected) {
			t.Fatalf("reconnecting: Run returned %v, want a RejectedError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a revoked agent was accepted")
	}
	stop()
}

func TestEnrollmentRefusals(t *testing.T) {
	h := startRemoteHub(t)
	ctx := context.Background()
	grant, err := h.CreateEnrollment(ctx, "admin", "")
	if err != nil {
		t.Fatal(err)
	}

	// A hub with another CA is refused before the token is sent, so the token
	// stays valid.
	other, _ := pki.LoadOrCreate(t.TempDir(), time.Now())
	if _, err := agent.Enroll(ctx, h.AgentAddr(), other.Fingerprint(), grant.Token, "1", t.TempDir()); err == nil {
		t.Fatal("enrolled with a fingerprint that does not match the hub")
	}
	if _, err := agent.Enroll(ctx, h.AgentAddr(), grant.CAFingerprint, "wrong-token", "1", t.TempDir()); status.Code(errors.Unwrap(err)) != codes.Unauthenticated {
		t.Errorf("wrong token: err = %v, want Unauthenticated", err)
	}
	if _, err := agent.Enroll(ctx, h.AgentAddr(), grant.CAFingerprint, grant.Token, "1", t.TempDir()); err != nil {
		t.Fatalf("the token should still work: %v", err)
	}
	if _, err := agent.Enroll(ctx, h.AgentAddr(), grant.CAFingerprint, grant.Token, "1", t.TempDir()); err == nil {
		t.Error("a token worked twice")
	}
}

func TestStreamsNeedAValidCertificate(t *testing.T) {
	h := startRemoteHub(t)
	creds := enrollNew(t, h, "")
	fp := pki.Fingerprint(creds.CADER)

	connect := func(cfg *tls.Config) error {
		conn, err := grpc.NewClient(h.AgentAddr(), grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		stream, err := agentv1.NewAgentServiceClient(conn).Connect(ctx)
		if err != nil {
			return err
		}
		_ = stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{ProtocolVersion: 1}}})
		_, err = stream.Recv()
		return err
	}

	// No certificate at all.
	anon, _ := pki.PinnedClientTLS(fp, nil)
	if err := connect(anon); status.Code(err) != codes.Unauthenticated {
		t.Errorf("without a certificate: err = %v, want Unauthenticated", err)
	}

	// A certificate from another CA never gets through the handshake.
	other, _ := pki.LoadOrCreate(t.TempDir(), time.Now())
	key, csr, _ := pki.NewAgentKey()
	der, _, _ := other.SignClient(csr, creds.AgentID, time.Now())
	forged, _ := pki.PinnedClientTLS(fp, &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if err := connect(forged); err == nil {
		t.Error("a certificate from another CA was accepted")
	}

	// A valid certificate may not claim another agent's id.
	good, _ := pki.PinnedClientTLS(fp, &tls.Certificate{Certificate: [][]byte{creds.CertDER}, PrivateKey: creds.Key})
	conn, err := grpc.NewClient(h.AgentAddr(), grpc.WithTransportCredentials(credentials.NewTLS(good)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := agentv1.NewAgentServiceClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{ProtocolVersion: 1, AgentId: BuiltinAgentID}}})
	msg, err := stream.Recv()
	if err != nil || msg.GetReject().GetReason() != agentv1.Reject_REASON_UNAUTHORIZED {
		t.Errorf("claiming another id: msg = %v, err = %v; want a Reject", msg, err)
	}
}

// api drives the HTTP handler of a hub as a logged-in user.
type api struct {
	t       *testing.T
	handler http.Handler
	cookie  *http.Cookie
}

func newAPI(t *testing.T, h *Hub, role auth.Role) *api {
	t.Helper()
	ctx := context.Background()
	admin, session, _, err := h.auth.Setup(ctx, h.auth.SetupToken(), "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if role != auth.RoleAdmin {
		if _, err := h.auth.CreateUser(ctx, admin, "viewer1", "correct horse battery", role); err != nil {
			t.Fatal(err)
		}
		_, session, _, err = h.auth.Login(ctx, "1.1.1.1", "viewer1", "correct horse battery")
		if err != nil {
			t.Fatal(err)
		}
	}
	return &api{
		t:       t,
		handler: NewHandler(Deps{UI: fstest.MapFS{}, Agents: h, Layouts: h, Log: quiet(), Auth: h.auth, Fleet: h, Updates: h.updates, Rollout: h, Version: h.version}),
		cookie:  &http.Cookie{Name: auth.CookieName, Value: session},
	}
}

func (a *api) do(method, path string, body any) *httptest.ResponseRecorder {
	a.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(a.cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestEnrollmentAPI(t *testing.T) {
	h := startRemoteHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)

	rec := admin.do("POST", "/api/enrollment-tokens", map[string]string{"name": "db1"})
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	var grant struct {
		Token         string `json:"token"`
		Name          string `json:"name"`
		CAFingerprint string `json:"ca_fingerprint"`
		AgentPort     int    `json:"agent_port"`
		ExpiresAt     string `json:"expires_at"`
		HubVersion    string `json:"hub_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &grant); err != nil || grant.Token == "" || grant.Name != "db1" || grant.AgentPort == 0 || grant.ExpiresAt == "" || grant.HubVersion != "9.9.9" {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	if !strings.HasSuffix(h.AgentAddr(), ":"+strconv.Itoa(grant.AgentPort)) {
		t.Errorf("agent_port = %d, listener at %s", grant.AgentPort, h.AgentAddr())
	}

	creds, err := agent.Enroll(context.Background(), h.AgentAddr(), grant.CAFingerprint, grant.Token, "1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rec := admin.do("POST", "/api/enrollment-tokens", map[string]string{"name": strings.Repeat("x", 100)}); rec.Code != http.StatusBadRequest {
		t.Errorf("long name: status = %d, want 400", rec.Code)
	}
	if rec := admin.do("DELETE", "/api/agents/agent-nope", struct{}{}); rec.Code != http.StatusNotFound {
		t.Errorf("revoke unknown: status = %d, want 404", rec.Code)
	}
	if rec := admin.do("DELETE", "/api/agents/"+BuiltinAgentID, struct{}{}); rec.Code != http.StatusNotFound {
		t.Errorf("revoke the built-in agent: status = %d, want 404", rec.Code)
	}
	if rec := admin.do("DELETE", "/api/agents/"+creds.AgentID, struct{}{}); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, body = %s", rec.Code, rec.Body)
	}
	cert, err := x509.ParseCertificate(creds.CertDER)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.fleet.Authorize(context.Background(), creds.AgentID, cert.SerialNumber.Text(16)); !errors.Is(err, fleet.ErrRevoked) {
		t.Errorf("after revoking: err = %v, want ErrRevoked", err)
	}
}

func TestEnrollmentAPIIsAdminOnly(t *testing.T) {
	h := startRemoteHub(t)
	viewer := newAPI(t, h, auth.RoleViewer)
	if rec := viewer.do("POST", "/api/enrollment-tokens", map[string]string{}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer creating a token: status = %d, want 403", rec.Code)
	}
	if rec := viewer.do("DELETE", "/api/agents/anything", struct{}{}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer revoking: status = %d, want 403", rec.Code)
	}
}

func TestEnrollmentAPIWithAgentsOff(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinAgent = false
	h := startHubWithConfig(t, cfg) // no agent_listen
	admin := newAPI(t, h, auth.RoleAdmin)
	if rec := admin.do("POST", "/api/enrollment-tokens", map[string]string{}); rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func (a *api) agents(query string) []agentJSON {
	a.t.Helper()
	rec := a.do("GET", "/api/agents"+query, nil)
	var list []agentJSON
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		a.t.Fatalf("GET /api/agents%s: status = %d, body = %s", query, rec.Code, rec.Body)
	}
	return list
}

func TestServerListShowsOfflineAndRevokedAgents(t *testing.T) {
	h := startRemoteHub(t)
	admin := newAPI(t, h, auth.RoleAdmin)
	creds := enrollNew(t, h, "web1")

	// Enrolled but never connected.
	list := admin.agents("?all=true")
	if len(list) != 1 || list[0].ID != creds.AgentID || list[0].Status != statusOffline || list[0].Name != "web1" || list[0].ConnectedAt != nil || list[0].LastSeenAt != nil {
		t.Fatalf("before connecting: %+v", list)
	}
	if got := admin.agents(""); len(got) != 0 {
		t.Errorf("without all=true only connected agents are listed: %+v", got)
	}

	stop, result := runAgent(t, h, creds)
	waitFor(t, "the agent to connect", func() bool { return len(h.Agents()) == 1 })
	list = admin.agents("?all=true")
	if len(list) != 1 || list[0].Status != statusConnected || list[0].Name != "web1" || list[0].ConnectedAt == nil {
		t.Fatalf("connected: %+v", list)
	}
	if got := admin.agents(""); len(got) != 1 || got[0].Name != "web1" {
		t.Errorf("the plain list should carry the name too: %+v", got)
	}

	// Gone: still listed, with when it was last seen and what it said it was.
	stop()
	<-result //nolint:errcheck // the stop error is not the point here
	waitFor(t, "the agent to disconnect", func() bool { return len(h.Agents()) == 0 })
	var off agentJSON
	waitFor(t, "the last seen time to be saved", func() bool {
		list = admin.agents("?all=true")
		off = list[0]
		return off.LastSeenAt != nil
	})
	if off.Status != statusOffline || off.Hostname == "" || off.Version != "9.9.9" || off.ConnectedAt != nil {
		t.Errorf("offline: %+v", off)
	}
	rec := admin.do("GET", "/api/agents/"+creds.AgentID, nil)
	var one agentJSON
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &one) != nil || one.Status != statusOffline || one.Name != "web1" {
		t.Errorf("GET one offline agent: status = %d, body = %s", rec.Code, rec.Body)
	}

	// Rename, then revoke.
	if rec := admin.do("PATCH", "/api/agents/"+creds.AgentID, map[string]string{"name": "Web One"}); rec.Code != http.StatusNoContent {
		t.Fatalf("rename: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := admin.do("PATCH", "/api/agents/"+creds.AgentID, map[string]string{"name": strings.Repeat("x", 80)}); rec.Code != http.StatusBadRequest {
		t.Errorf("rename too long: status = %d, want 400", rec.Code)
	}
	if rec := admin.do("PATCH", "/api/agents/nope", map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("rename unknown: status = %d, want 404", rec.Code)
	}
	if rec := admin.do("DELETE", "/api/agents/"+creds.AgentID, struct{}{}); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d", rec.Code)
	}
	if list := admin.agents("?all=true"); len(list) != 1 || list[0].Status != statusRevoked || list[0].Name != "Web One" {
		t.Errorf("revoked: %+v", list)
	}
}

func TestRenameIsAdminOnly(t *testing.T) {
	h := startRemoteHub(t)
	viewer := newAPI(t, h, auth.RoleViewer)
	if rec := viewer.do("PATCH", "/api/agents/x", map[string]string{"name": "n"}); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// The API checks permissions, not role names: an operator may manage servers,
// a role with nothing may only use what any signed-in user may.
func TestAPIChecksPermissions(t *testing.T) {
	h := startRemoteHub(t)
	ctx := context.Background()
	admin, _, _, err := h.auth.Setup(ctx, h.auth.SetupToken(), "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.auth.CreateRole(ctx, admin, "nothing", "", nil); err != nil {
		t.Fatal(err)
	}
	as := func(username string, role auth.Role) *api {
		t.Helper()
		if _, err := h.auth.CreateUser(ctx, admin, username, "correct horse battery", role); err != nil {
			t.Fatal(err)
		}
		_, session, _, err := h.auth.Login(ctx, "1.1.1.1", username, "correct horse battery")
		if err != nil {
			t.Fatal(err)
		}
		return &api{
			t:       t,
			handler: NewHandler(Deps{UI: fstest.MapFS{}, Agents: h, Layouts: h, Log: quiet(), Auth: h.auth, Fleet: h, Updates: h.updates, Rollout: h}),
			cookie:  &http.Cookie{Name: auth.CookieName, Value: session},
		}
	}
	for _, tc := range []struct {
		who          *api
		method, path string
		body         any
		want         int
	}{
		{as("olivia", auth.RoleOperator), "GET", "/api/agents", nil, http.StatusOK},
		{as("olivia2", auth.RoleOperator), "POST", "/api/enrollment-tokens", map[string]string{}, http.StatusCreated},
		{as("olivia3", auth.RoleOperator), "PUT", "/api/layout", map[string]any{"layout": map[string]any{}}, http.StatusNoContent},
		{as("nora", "nothing"), "GET", "/api/agents", nil, http.StatusForbidden},
		{as("nora2", "nothing"), "GET", "/api/agents/local/metrics", nil, http.StatusForbidden},
		{as("nora3", "nothing"), "PUT", "/api/layout", map[string]any{"layout": map[string]any{}}, http.StatusForbidden},
		{as("nora4", "nothing"), "GET", "/api/layout", nil, http.StatusOK},
		{as("vic", auth.RoleViewer), "POST", "/api/enrollment-tokens", map[string]string{}, http.StatusForbidden},
		{as("vic2", auth.RoleViewer), "PATCH", "/api/agents/local", map[string]string{"name": "x"}, http.StatusForbidden},
		// Updating the core is not part of managing servers.
		{as("olivia4", auth.RoleOperator), "GET", "/api/updates", nil, http.StatusForbidden},
		{as("olivia5", auth.RoleOperator), "POST", "/api/updates/check", struct{}{}, http.StatusForbidden},
		{as("vic3", auth.RoleViewer), "PUT", "/api/updates/settings", map[string]string{"mode": "manual", "channel": "stable"}, http.StatusForbidden},
	} {
		if rec := tc.who.do(tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s: status = %d, want %d (%s)", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
	}
}

// freePort returns a port nothing listens on right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func decodeListener(t *testing.T, rec *httptest.ResponseRecorder) AgentListener {
	t.Helper()
	var st AgentListener
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	return st
}

func TestTurningRemoteAgentsOnFromTheAPI(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinAgent = false
	h := startHubWithConfig(t, cfg) // no agent_listen
	admin := newAPI(t, h, auth.RoleAdmin)

	rec := admin.do("GET", "/api/agents/listener", nil)
	if st := decodeListener(t, rec); rec.Code != http.StatusOK || st.Enabled || !st.Editable || st.CAFingerprint == "" {
		t.Fatalf("off: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := admin.do("PUT", "/api/agents/listener", map[string]int{"port": 80}); rec.Code != http.StatusBadRequest {
		t.Errorf("privileged port: status = %d, want 400", rec.Code)
	}

	port := freePort(t)
	rec = admin.do("PUT", "/api/agents/listener", map[string]int{"port": port})
	st := decodeListener(t, rec)
	if rec.Code != http.StatusOK || !st.Enabled || st.Port != port || st.Source != ListenerFromDashboard || st.Editable {
		t.Fatalf("on: status = %d, body = %s", rec.Code, rec.Body)
	}
	if !strings.HasSuffix(h.AgentAddr(), ":"+strconv.Itoa(port)) {
		t.Errorf("listener at %q, want port %d", h.AgentAddr(), port)
	}
	if rec := admin.do("PUT", "/api/agents/listener", map[string]int{"port": port}); rec.Code != http.StatusConflict {
		t.Errorf("turning it on twice: status = %d, want 409", rec.Code)
	}
	if rec := admin.do("POST", "/api/enrollment-tokens", map[string]string{}); rec.Code != http.StatusCreated {
		t.Errorf("token once agents are on: status = %d, body = %s", rec.Code, rec.Body)
	}

	// Kept for the next start, and audited.
	if saved, err := h.savedAgentListen(t.Context()); err != nil || saved != ":"+strconv.Itoa(port) {
		t.Errorf("saved = %q, %v", saved, err)
	}
	entries, err := h.auth.Audit(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		found = found || (e.Event == EventAgentsListenerOn && e.Actor == "admin")
	}
	if !found {
		t.Errorf("no %s event by admin in %+v", EventAgentsListenerOn, entries)
	}
}

func TestRemoteAgentsListenerSetInTheConfigurationWins(t *testing.T) {
	h := startRemoteHub(t) // agent_listen in the configuration
	admin := newAPI(t, h, auth.RoleAdmin)
	rec := admin.do("GET", "/api/agents/listener", nil)
	if st := decodeListener(t, rec); !st.Enabled || st.Source != ListenerFromConfig || st.Editable {
		t.Errorf("body = %s", rec.Body)
	}
	if rec := admin.do("PUT", "/api/agents/listener", map[string]int{"port": freePort(t)}); rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestOnlyServerManagersTurnRemoteAgentsOn(t *testing.T) {
	cfg := config.Default()
	cfg.BuiltinAgent = false
	h := startHubWithConfig(t, cfg)
	viewer := newAPI(t, h, auth.RoleViewer)
	if rec := viewer.do("GET", "/api/agents/listener", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer reading: status = %d, want 200", rec.Code)
	}
	if rec := viewer.do("PUT", "/api/agents/listener", map[string]int{"port": freePort(t)}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer turning on: status = %d, want 403", rec.Code)
	}
}
