package metrics

import (
	"sync"
	"sync/atomic"
)

// RouteMetrics holds per-route counters.
// All fields accessed atomically — no mutex on the hot path.
type RouteMetrics struct {
	Requests       atomic.Int64
	Errors         atomic.Int64
	TotalLatencyNs atomic.Int64
	statusCodes    [6]atomic.Int64
}

// RecordRequest increments the request counter.
func (m *RouteMetrics) RecordRequest() { m.Requests.Add(1) }

// RecordError increments the error counter.
func (m *RouteMetrics) RecordError() { m.Errors.Add(1) }

// RecordLatency adds ns to the cumulative latency counter.
func (m *RouteMetrics) RecordLatency(ns int64) { m.TotalLatencyNs.Add(ns) }

// RecordStatus increments the counter for the given HTTP status code.
func (m *RouteMetrics) RecordStatus(code int) {
	m.statusCodes[statusBucket(code)].Add(1)
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *RouteMetrics) Snapshot() Snapshot {
	requests := m.Requests.Load()
	totalLatency := m.TotalLatencyNs.Load()

	var avgLatency int64
	if requests > 0 {
		avgLatency = totalLatency / requests
	}

	return Snapshot{
		Requests:       requests,
		Errors:         m.Errors.Load(),
		TotalLatencyNs: totalLatency,
		AvgLatencyNs:   avgLatency,
		Status1xx:      m.statusCodes[0].Load(),
		Status2xx:      m.statusCodes[1].Load(),
		Status3xx:      m.statusCodes[2].Load(),
		Status4xx:      m.statusCodes[3].Load(),
		Status5xx:      m.statusCodes[4].Load(),
		StatusOther:    m.statusCodes[5].Load(),
	}
}

// Snapshot is a point-in-time copy of RouteMetrics.
type Snapshot struct {
	Requests       int64 `json:"requests"`
	Errors         int64 `json:"errors"`
	TotalLatencyNs int64 `json:"total_latency_ns"`
	AvgLatencyNs   int64 `json:"avg_latency_ns"`
	Status1xx      int64 `json:"status_1xx"`
	Status2xx      int64 `json:"status_2xx"`
	Status3xx      int64 `json:"status_3xx"`
	Status4xx      int64 `json:"status_4xx"`
	Status5xx      int64 `json:"status_5xx"`
	StatusOther    int64 `json:"status_other"`
}

// statusBucket maps an HTTP status code to a slice index.
func statusBucket(code int) int {
	switch {
	case code >= 100 && code < 200:
		return 0
	case code >= 200 && code < 300:
		return 1
	case code >= 300 && code < 400:
		return 2
	case code >= 400 && code < 500:
		return 3
	case code >= 500 && code < 600:
		return 4
	default:
		return 5
	}
}

// Collector manages per-route metrics.
// Safe for concurrent use.
type Collector struct {
	mu      sync.RWMutex
	metrics map[string]*RouteMetrics
}

// NewCollector creates an empty Collector.
func NewCollector() *Collector {
	return &Collector{
		metrics: make(map[string]*RouteMetrics),
	}
}

// GetOrCreate returns the RouteMetrics for routeID, creating it if absent.
func (c *Collector) GetOrCreate(routeID string) *RouteMetrics {
	c.mu.RLock()
	m, ok := c.metrics[routeID]
	c.mu.RUnlock()
	if ok {
		return m
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok = c.metrics[routeID]; ok {
		return m
	}
	m = &RouteMetrics{}
	c.metrics[routeID] = m
	return m
}

// Remove deletes the metrics for routeID.
func (c *Collector) Remove(routeID string) {
	c.mu.Lock()
	delete(c.metrics, routeID)
	c.mu.Unlock()
}

// Snapshot returns a point-in-time copy of all route metrics.
func (c *Collector) Snapshot() map[string]Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[string]Snapshot, len(c.metrics))
	for id, m := range c.metrics {
		out[id] = m.Snapshot()
	}
	return out
}
