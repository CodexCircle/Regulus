package hub

import (
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// sample is a report taken at `taken` (what the agent says) and received at `got`.
func sample(taken, got time.Time, rx uint64) MetricsSample {
	r := &agentv1.MetricsReport{Networks: []*agentv1.NetworkMetrics{{Interface: "eth0", RxBytes: rx, TxBytes: rx}}}
	if !taken.IsZero() {
		r.CollectedAt = timestamppb.New(taken)
	}
	return MetricsSample{At: got, Report: r}
}

func rxRate(t *testing.T, s MetricsSample, prev MetricsSample) *float64 {
	t.Helper()
	return toPoint(s, &prev).Networks[0].RxBytesPerSec
}

func TestRateUsesTheTimesTheAgentTookTheReports(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	// A stall delivers two reports taken ten seconds apart one millisecond
	// apart: 10 000 bytes in 10 s is 1000 B/s, not 10 MB/s.
	got := rxRate(t, sample(t0.Add(10*time.Second), t0.Add(30*time.Second+time.Millisecond), 10_000),
		sample(t0, t0.Add(30*time.Second), 0))
	if got == nil || math.Abs(*got-1000) > 1e-6 {
		t.Errorf("rate after a burst = %v, want 1000", got)
	}

	// The opposite: reports taken 10 s apart but delivered 40 s apart.
	got = rxRate(t, sample(t0.Add(10*time.Second), t0.Add(50*time.Second), 10_000), sample(t0, t0.Add(10*time.Second), 0))
	if got == nil || math.Abs(*got-1000) > 1e-6 {
		t.Errorf("rate after a late delivery = %v, want 1000", got)
	}
}

func TestRateIsLeftOutWhenItCannotBeTrusted(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct{ taken, prevTaken time.Time }{
		"the same instant":             {t0, t0},
		"the agent's clock went back":  {t0.Add(-time.Hour), t0},
		"a gap of more than the limit": {t0.Add(maxRateGap + time.Second), t0},
	} {
		if got := rxRate(t, sample(tc.taken, t0.Add(10*time.Second), 5000), sample(tc.prevTaken, t0, 0)); got != nil {
			t.Errorf("%s: rate = %v, want none", name, *got)
		}
	}
	// Right at the limit it is still computed.
	if got := rxRate(t, sample(t0.Add(maxRateGap), t0.Add(10*time.Second), 3000), sample(t0, t0, 0)); got == nil {
		t.Error("no rate at exactly the limit")
	}
}

func TestRateFallsBackToReceiveTimesForReportsWithoutATime(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	got := rxRate(t, sample(time.Time{}, t0.Add(10*time.Second), 5000), sample(time.Time{}, t0, 0))
	if got == nil || math.Abs(*got-500) > 1e-6 {
		t.Errorf("rate = %v, want 500", got)
	}
}
