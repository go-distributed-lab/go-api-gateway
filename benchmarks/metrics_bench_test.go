package benchmarks

import (
	"testing"

	"go-api-gateway/internal/metrics"
)

func BenchmarkMetrics_RecordRequest(b *testing.B) {
	m := &metrics.RouteMetrics{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordRequest()
	}
}

func BenchmarkMetrics_RecordAll(b *testing.B) {
	m := &metrics.RouteMetrics{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordRequest()
		m.RecordLatency(1000)
		m.RecordStatus(200)
	}
}

func BenchmarkMetrics_Snapshot(b *testing.B) {
	m := &metrics.RouteMetrics{}
	m.RecordRequest()
	m.RecordLatency(1000)
	m.RecordStatus(200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Snapshot()
	}
}

func BenchmarkCollector_GetOrCreate(b *testing.B) {
	c := metrics.NewCollector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.GetOrCreate("route-a")
	}
}

func BenchmarkCollector_Snapshot(b *testing.B) {
	c := metrics.NewCollector()
	for _, id := range []string{"r1", "r2", "r3", "r4", "r5"} {
		m := c.GetOrCreate(id)
		m.RecordRequest()
		m.RecordLatency(500)
		m.RecordStatus(200)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Snapshot()
	}
}
