package hub

import (
	"encoding/json"
	"sync"
	"time"
)

// subscriberBuffer is how many events a subscriber may fall behind before it
// is dropped. A dropped client reconnects and gets a fresh snapshot.
const subscriberBuffer = 64

// broker fans events out, as JSON, to the WebSocket clients.
type broker struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newBroker() *broker {
	return &broker{subs: map[chan []byte]struct{}{}}
}

// subscribe returns a channel of events and a function that ends the
// subscription. The channel is closed when the subscriber is too slow or when
// it is cancelled.
func (b *broker) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() { b.drop(ch) }
}

func (b *broker) drop(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}

// publish sends v to every subscriber without ever blocking: a subscriber whose
// buffer is full is dropped.
func (b *broker) publish(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- data:
		default:
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Messages sent on GET /api/stream. Every message has a "type".
type (
	// snapshotEvent is sent first, with the agents connected at that moment.
	snapshotEvent struct {
		Type              string      `json:"type"` // "snapshot"
		StaleAfterSeconds float64     `json:"stale_after_seconds"`
		Agents            []agentJSON `json:"agents"`
	}
	// agentEvent: "agent.connected" (also when an agent reconnects).
	agentEvent struct {
		Type  string    `json:"type"`
		Agent agentJSON `json:"agent"`
	}
	// goneEvent: "agent.disconnected".
	goneEvent struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	// heartbeatEvent: "agent.heartbeat".
	heartbeatEvent struct {
		Type string    `json:"type"`
		ID   string    `json:"id"`
		At   time.Time `json:"at"`
	}
	// metricsEvent: "agent.metrics", one point like those of the metrics endpoint.
	metricsEvent struct {
		Type  string    `json:"type"`
		ID    string    `json:"id"`
		Point pointJSON `json:"point"`
	}
)
