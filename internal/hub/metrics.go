package hub

import (
	"sync"
	"time"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

const (
	// defaultMetricsRetention is how long the hub keeps the metrics of an agent.
	defaultMetricsRetention = time.Hour
	// maxMetricsSamples bounds the memory used per agent whatever the interval.
	maxMetricsSamples = 3600
)

// MetricsSample is one metrics report with the time the hub received it.
type MetricsSample struct {
	At     time.Time
	Report *agentv1.MetricsReport
}

// metricsStore keeps the recent metrics of every agent in memory. The history
// of an agent survives its reconnections and is dropped once it is older than
// the retention; it is lost when the hub restarts.
type metricsStore struct {
	retention time.Duration
	now       func() time.Time

	mu      sync.Mutex
	samples map[string][]MetricsSample
}

func newMetricsStore(retention time.Duration) *metricsStore {
	return &metricsStore{retention: retention, now: time.Now, samples: map[string][]MetricsSample{}}
}

// add stores a report and returns it as a sample together with the previous
// sample of the agent, if any (needed to compute rates).
func (m *metricsStore) add(agentID string, report *agentv1.MetricsReport) (sample MetricsSample, prev *MetricsSample) {
	if report == nil {
		return MetricsSample{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	sample = MetricsSample{At: now, Report: report}
	if old := m.samples[agentID]; len(old) > 0 {
		last := old[len(old)-1]
		prev = &last
	}
	list := append(m.samples[agentID], sample)
	if over := len(list) - maxMetricsSamples; over > 0 {
		list = list[over:]
	}
	m.samples[agentID] = list
	m.pruneLocked(now)
	return sample, prev
}

// pruneLocked drops the samples older than the retention, for every agent, so
// that agents that never come back do not keep memory forever.
func (m *metricsStore) pruneLocked(now time.Time) {
	cutoff := now.Add(-m.retention)
	for id, list := range m.samples {
		i := 0
		for i < len(list) && list[i].At.Before(cutoff) {
			i++
		}
		switch {
		case i == len(list):
			delete(m.samples, id)
		case i > 0:
			m.samples[id] = append([]MetricsSample(nil), list[i:]...)
		}
	}
}

// history returns a copy of the samples of the agent, oldest first.
func (m *metricsStore) history(agentID string) []MetricsSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(m.now())
	return append([]MetricsSample(nil), m.samples[agentID]...)
}

func (m *metricsStore) known(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.samples[agentID]
	return ok
}
