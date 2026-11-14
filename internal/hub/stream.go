package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
)

// sessionRecheck is how often an open stream asks whether its session and
// permission still stand (a variable so that a test need not wait).
var sessionRecheck = 30 * time.Second

// streamHandler upgrades GET /api/stream to a WebSocket and pushes JSON
// events: first a "snapshot" of the connected agents, then "agent.connected",
// "agent.disconnected", "agent.heartbeat" and "agent.metrics" as they happen.
// The client sends nothing. The upgrade refuses a cross-origin page, and the
// session cookie is checked before the handler runs, and again every
// sessionRecheck: a stream whose session ended (logout, disabled account, new
// password) or whose user lost the permission is closed. still makes that
// check for one request; nil means no login is needed.
func streamHandler(src AgentSource, still func(*http.Request) func(context.Context) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if src == nil {
			writeJSONError(w, http.StatusNotFound, "stream is not available")
			return
		}
		// Subscribe before taking the snapshot so no event falls in between;
		// an event already included in the snapshot is harmless to repeat.
		events, cancel := src.Subscribe()
		defer cancel()

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept has already answered
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := conn.CloseRead(r.Context()) // handles pings and the close handshake

		now, staleAfter := time.Now(), src.StaleAfter()
		agents := []agentJSON{}
		for _, a := range src.Agents() {
			agents = append(agents, toAgentJSON(a, now, staleAfter))
		}
		first, _ := json.Marshal(snapshotEvent{Type: "snapshot", StaleAfterSeconds: staleAfter.Seconds(), Agents: agents})
		if !send(ctx, conn, first) {
			return
		}

		ping := time.NewTicker(pingInterval)
		defer ping.Stop()
		allowed := func(context.Context) bool { return true }
		if still != nil {
			allowed = still(r)
		}
		recheck := time.NewTicker(sessionRecheck)
		defer recheck.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-recheck.C:
				if !allowed(ctx) {
					_ = conn.Close(websocket.StatusPolicyViolation, "session ended")
					return
				}
			case msg, ok := <-events:
				if !ok { // too slow: close so that the client reconnects and resyncs
					_ = conn.Close(websocket.StatusPolicyViolation, "too slow")
					return
				}
				if !send(ctx, conn, msg) {
					return
				}
			case <-ping.C:
				pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}

func send(ctx context.Context, conn *websocket.Conn, msg []byte) bool {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg) == nil
}
