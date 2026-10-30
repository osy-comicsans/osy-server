package observability

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	startedAt        time.Time
	requestsTotal    atomic.Int64
	requestsFailed   atomic.Int64
	requestsInFlight atomic.Int64
}

type MetricsSnapshot struct {
	UptimeSeconds    int64 `json:"uptime_seconds"`
	RequestsTotal    int64 `json:"requests_total"`
	RequestsFailed   int64 `json:"requests_failed"`
	RequestsInFlight int64 `json:"requests_in_flight"`
}

func NewMetrics() *Metrics {
	return &Metrics{startedAt: time.Now()}
}

func (m *Metrics) RequestStarted() {
	m.requestsInFlight.Add(1)
	m.requestsTotal.Add(1)
}

func (m *Metrics) RequestFinished(failed bool) {
	m.requestsInFlight.Add(-1)
	if failed {
		m.requestsFailed.Add(1)
	}
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		UptimeSeconds:    int64(time.Since(m.startedAt).Seconds()),
		RequestsTotal:    m.requestsTotal.Load(),
		RequestsFailed:   m.requestsFailed.Load(),
		RequestsInFlight: m.requestsInFlight.Load(),
	}
}
