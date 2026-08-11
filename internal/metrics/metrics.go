package metrics

import "sync/atomic"

// RouteMetrics holds per-route counters.
// All fields are accessed atomically — no mutex required on the hot path.
type RouteMetrics struct {
	// Requests is the total number of requests received for this route.
	Requests atomic.Int64

	// Errors is the total number of upstream errors (5xx or network failure).
	Errors atomic.Int64

	// TotalLatencyNs is the cumulative upstream latency in nanoseconds.
	// Divide by Requests to get average latency.
	TotalLatencyNs atomic.Int64

	// StatusCodes counts responses by HTTP status code.
	// Key: status code (200, 404, 502, …), Value: count.
	// Protected by its own mutex — not on the hot path.
	statusCodes [6]atomic.Int64 // index = statusBucket(code)
}

// RecordRequest increments the request counter.
func (m *RouteMetrics) RecordRequest() {
	m.Requests.Add(1)
}

// RecordError increments the error counter.
func (m *RouteMetrics) RecordError() {
	m.Errors.Add(1)
}

// RecordLatency adds ns to the cumulative latency counter.
func (m *RouteMetrics) RecordLatency(ns int64) {
	m.TotalLatencyNs.Add(ns)
}

// RecordStatus increments the counter for the given HTTP status code.
func (m *RouteMetrics) RecordStatus(code int) {
	m.statusCodes[statusBucket(code)].Add(1)
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *RouteMetrics) Snapshot() Snapshot {
	return Snapshot{
		Requests:       m.Requests.Load(),
		Errors:         m.Errors.Load(),
		TotalLatencyNs: m.TotalLatencyNs.Load(),
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
	Requests       int64
	Errors         int64
	TotalLatencyNs int64
	Status1xx      int64
	Status2xx      int64
	Status3xx      int64
	Status4xx      int64
	Status5xx      int64
	StatusOther    int64
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
