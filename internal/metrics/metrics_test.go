package metrics

import (
	"sync"
	"testing"
)

func TestRouteMetrics_RecordRequest(t *testing.T) {
	m := &RouteMetrics{}
	m.RecordRequest()
	m.RecordRequest()
	if m.Requests.Load() != 2 {
		t.Fatalf("expected 2, got %d", m.Requests.Load())
	}
}

func TestRouteMetrics_RecordError(t *testing.T) {
	m := &RouteMetrics{}
	m.RecordError()
	if m.Errors.Load() != 1 {
		t.Fatalf("expected 1, got %d", m.Errors.Load())
	}
}

func TestRouteMetrics_RecordLatency(t *testing.T) {
	m := &RouteMetrics{}
	m.RecordLatency(1000)
	m.RecordLatency(2000)
	if m.TotalLatencyNs.Load() != 3000 {
		t.Fatalf("expected 3000, got %d", m.TotalLatencyNs.Load())
	}
}

func TestRouteMetrics_RecordStatus(t *testing.T) {
	m := &RouteMetrics{}
	m.RecordStatus(200)
	m.RecordStatus(201)
	m.RecordStatus(404)
	m.RecordStatus(500)
	m.RecordStatus(101)
	m.RecordStatus(301)

	s := m.Snapshot()
	if s.Status2xx != 2 {
		t.Errorf("expected 2xx=2, got %d", s.Status2xx)
	}
	if s.Status4xx != 1 {
		t.Errorf("expected 4xx=1, got %d", s.Status4xx)
	}
	if s.Status5xx != 1 {
		t.Errorf("expected 5xx=1, got %d", s.Status5xx)
	}
	if s.Status1xx != 1 {
		t.Errorf("expected 1xx=1, got %d", s.Status1xx)
	}
	if s.Status3xx != 1 {
		t.Errorf("expected 3xx=1, got %d", s.Status3xx)
	}
}

func TestRouteMetrics_Snapshot_AvgLatency(t *testing.T) {
	m := &RouteMetrics{}
	m.RecordRequest()
	m.RecordRequest()
	m.RecordLatency(1000)
	m.RecordLatency(3000)

	s := m.Snapshot()
	if s.AvgLatencyNs != 2000 {
		t.Fatalf("expected avg=2000, got %d", s.AvgLatencyNs)
	}
}

func TestCollector_GetOrCreate(t *testing.T) {
	c := NewCollector()
	m1 := c.GetOrCreate("route-a")
	m2 := c.GetOrCreate("route-a")
	if m1 != m2 {
		t.Fatal("expected same pointer for same route ID")
	}
}

func TestCollector_GetOrCreate_Different(t *testing.T) {
	c := NewCollector()
	m1 := c.GetOrCreate("route-a")
	m2 := c.GetOrCreate("route-b")
	if m1 == m2 {
		t.Fatal("expected different pointers for different route IDs")
	}
}

func TestCollector_Remove(t *testing.T) {
	c := NewCollector()
	c.GetOrCreate("route-a")
	c.Remove("route-a")
	snap := c.Snapshot()
	if _, ok := snap["route-a"]; ok {
		t.Fatal("expected route-a to be removed")
	}
}

func TestCollector_Snapshot(t *testing.T) {
	c := NewCollector()
	m := c.GetOrCreate("route-a")
	m.RecordRequest()
	m.RecordLatency(500)
	m.RecordStatus(200)

	snap := c.Snapshot()
	s, ok := snap["route-a"]
	if !ok {
		t.Fatal("expected route-a in snapshot")
	}
	if s.Requests != 1 {
		t.Errorf("expected 1 request, got %d", s.Requests)
	}
	if s.Status2xx != 1 {
		t.Errorf("expected 1 2xx, got %d", s.Status2xx)
	}
}

func TestCollector_Concurrent(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			m := c.GetOrCreate("shared-route")
			m.RecordRequest()
			m.RecordLatency(100)
			m.RecordStatus(200)
		}(i)
	}
	wg.Wait()

	snap := c.Snapshot()
	if snap["shared-route"].Requests != 100 {
		t.Fatalf("expected 100 requests, got %d", snap["shared-route"].Requests)
	}
}
