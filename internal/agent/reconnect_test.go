package agent

import (
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// recordDelays replaces the jitter with a function that records the delay it is
// asked for and returns it shortened to a few milliseconds, so that a test
// sees the sequence of delays without waiting for them.
func recordDelays(t *testing.T) func() []time.Duration {
	t.Helper()
	var mu sync.Mutex
	var got []time.Duration
	old := reconnectJitter
	reconnectJitter = func(d time.Duration) time.Duration {
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
		return time.Millisecond
	}
	t.Cleanup(func() { reconnectJitter = old })
	return func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func waitForDelays(t *testing.T, delays func() []time.Duration, n int) []time.Duration {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if d := delays(); len(d) >= n {
			return d[:n]
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d retries after 5 s, want %d", len(delays()), n)
		}
		time.Sleep(time.Millisecond)
	}
}

type stream = grpc.BidiStreamingServer[agentv1.AgentMessage, agentv1.HubMessage]

// A hub that accepts the agent and drops it at once must not make the agent
// retry every second forever: the delay has to grow, as it does when the hub
// refuses the connection.
func TestDelayGrowsWhenTheHubAcceptsAndDropsTheAgent(t *testing.T) {
	delays := recordDelays(t)
	conn := newFakeHub(t, func(_ int, s stream) error {
		if _, err := s.Recv(); err != nil {
			return err
		}
		if err := s.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		return nil // the stream ends right after Welcome
	})
	o := opts()
	o.MinBackoff, o.MaxBackoff = 10*time.Millisecond, 80*time.Millisecond
	runAgent(t, conn, o)

	got := waitForDelays(t, delays, 6)
	want := []time.Duration{10, 20, 40, 80, 80, 80}
	for i := range want {
		if got[i] != want[i]*time.Millisecond {
			t.Fatalf("delays = %v, want %v ms", got, want)
		}
	}
}

// A connection that held for a while starts the delays over.
func TestDelayStartsOverAfterAConnectionThatHeld(t *testing.T) {
	delays := recordDelays(t)
	old := stableAfter
	stableAfter = 30 * time.Millisecond
	t.Cleanup(func() { stableAfter = old })
	conn := newFakeHub(t, func(_ int, s stream) error {
		if _, err := s.Recv(); err != nil {
			return err
		}
		if err := s.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		time.Sleep(80 * time.Millisecond) // longer than stableAfter
		return nil
	})
	o := opts()
	o.MinBackoff, o.MaxBackoff = 10*time.Millisecond, 80*time.Millisecond
	runAgent(t, conn, o)

	for i, d := range waitForDelays(t, delays, 4) {
		if d != 10*time.Millisecond {
			t.Errorf("retry %d waited %v after a connection that held, want the shortest delay", i+1, d)
		}
	}
}

// Two machines with one identity: the hub closes the older stream with
// AlreadyExists. The agent must wait the longest delay, not one second.
func TestAgentWaitsTheLongestWhenItsIdentityIsInUse(t *testing.T) {
	delays := recordDelays(t)
	conn := newFakeHub(t, func(_ int, s stream) error {
		if _, err := s.Recv(); err != nil {
			return err
		}
		if err := s.Send(welcome("a", time.Hour, time.Hour)); err != nil {
			return err
		}
		return status.Error(codes.AlreadyExists, "replaced by a newer connection of the same agent")
	})
	o := opts()
	o.MinBackoff, o.MaxBackoff = 10*time.Millisecond, 500*time.Millisecond
	runAgent(t, conn, o)

	for i, d := range waitForDelays(t, delays, 3) {
		if d != 500*time.Millisecond {
			t.Errorf("retry %d waited %v with the identity in use, want the longest delay", i+1, d)
		}
	}
}

func TestJitterSpreadsTheRetries(t *testing.T) {
	d := 10 * time.Second
	seen := map[time.Duration]bool{}
	for range 2000 {
		got := reconnectJitter(d)
		if got < d/2 || got > d {
			t.Fatalf("jitter(%v) = %v, want between %v and %v", d, got, d/2, d)
		}
		seen[got] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d different delays in 2000 draws: the retries would still move together", len(seen))
	}
	if got := reconnectJitter(0); got != 0 {
		t.Errorf("jitter(0) = %v", got)
	}
}
