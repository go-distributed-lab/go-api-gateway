package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-api-gateway/pkg/gateway"
)

// helpers

func makeRoute(id, method, path, upstream string) gateway.Route {
	return gateway.Route{
		ID:       id,
		Method:   method,
		Path:     path,
		Upstream: upstream,
	}
}

func makeRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	return r
}

// --- Add ---

func TestAdd_Valid(t *testing.T) {
	rt := New()
	err := rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestAdd_Duplicate(t *testing.T) {
	rt := New()
	route := makeRoute("r1", "GET", "/api/users", "http://localhost:9001")
	_ = rt.Add(route)
	err := rt.Add(route)
	if err != gateway.ErrRouteDuplicate {
		t.Fatalf("expected ErrRouteDuplicate, got %v", err)
	}
}

func TestAdd_InvalidRoute(t *testing.T) {
	rt := New()

	cases := []gateway.Route{
		{ID: "", Path: "/x", Upstream: "http://x"}, // missing ID
		{ID: "r1", Path: "", Upstream: "http://x"}, // missing Path
		{ID: "r2", Path: "/x", Upstream: ""},       // missing Upstream
	}

	for _, c := range cases {
		if err := rt.Add(c); err != gateway.ErrInvalidRoute {
			t.Errorf("expected ErrInvalidRoute for %+v, got %v", c, err)
		}
	}
}

// --- Remove ---

func TestRemove_Existing(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))
	err := rt.Remove("r1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRemove_NotFound(t *testing.T) {
	rt := New()
	err := rt.Remove("nonexistent")
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound, got %v", err)
	}
}

func TestRemove_ThenMatch(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))
	_ = rt.Remove("r1")

	_, err := rt.Match(makeRequest("GET", "/api/users"))
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound after remove, got %v", err)
	}
}

// --- Match: exact ---

func TestMatch_ExactMethod(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))

	m, err := rt.Match(makeRequest("GET", "/api/users"))
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if m.Route.ID != "r1" {
		t.Fatalf("expected r1, got %s", m.Route.ID)
	}
}

func TestMatch_ExactMethodMismatch(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))

	_, err := rt.Match(makeRequest("POST", "/api/users"))
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound on method mismatch, got %v", err)
	}
}

func TestMatch_AnyMethod(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "", "/api/users", "http://localhost:9001"))

	for _, method := range []string{"GET", "POST", "DELETE", "PATCH"} {
		m, err := rt.Match(makeRequest(method, "/api/users"))
		if err != nil {
			t.Fatalf("method %s: expected match, got %v", method, err)
		}
		if m.Route.ID != "r1" {
			t.Fatalf("method %s: expected r1, got %s", method, m.Route.ID)
		}
	}
}

func TestMatch_ExactBeforePrefix(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("prefix", "GET", "/api/", "http://prefix"))
	_ = rt.Add(makeRoute("exact", "GET", "/api/users", "http://exact"))

	m, err := rt.Match(makeRequest("GET", "/api/users"))
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if m.Route.ID != "exact" {
		t.Fatalf("exact should win over prefix, got %s", m.Route.ID)
	}
}

func TestMatch_NotFound(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/api/users", "http://localhost:9001"))

	_, err := rt.Match(makeRequest("GET", "/api/orders"))
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound, got %v", err)
	}
}

// --- Match: prefix ---

func TestMatch_PrefixBasic(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("api", "GET", "/api/", "http://api-upstream"))

	m, err := rt.Match(makeRequest("GET", "/api/anything/here"))
	if err != nil {
		t.Fatalf("expected prefix match, got %v", err)
	}
	if m.Route.ID != "api" {
		t.Fatalf("expected api, got %s", m.Route.ID)
	}
}

func TestMatch_LongestPrefixWins(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("short", "GET", "/api/", "http://short"))
	_ = rt.Add(makeRoute("long", "GET", "/api/v1/", "http://long"))

	m, err := rt.Match(makeRequest("GET", "/api/v1/users"))
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if m.Route.ID != "long" {
		t.Fatalf("longest prefix should win, got %s", m.Route.ID)
	}
}

func TestMatch_PrefixMethodMismatch(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("api", "POST", "/api/", "http://api-upstream"))

	_, err := rt.Match(makeRequest("GET", "/api/users"))
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound on prefix method mismatch, got %v", err)
	}
}

func TestMatch_CatchAll(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("catchall", "", "/", "http://fallback"))
	_ = rt.Add(makeRoute("specific", "GET", "/api/users", "http://specific"))

	// Specific route wins
	m, err := rt.Match(makeRequest("GET", "/api/users"))
	if err != nil || m.Route.ID != "specific" {
		t.Fatalf("specific route should win, got %v / %v", m.Route.ID, err)
	}

	// Unknown path falls to catch-all
	m, err = rt.Match(makeRequest("DELETE", "/unknown/path"))
	if err != nil || m.Route.ID != "catchall" {
		t.Fatalf("catch-all should match, got %v / %v", m.Route.ID, err)
	}
}

// --- Routes snapshot ---

func TestRoutes_Snapshot(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/a", "http://a"))
	_ = rt.Add(makeRoute("r2", "POST", "/b", "http://b"))
	_ = rt.Add(makeRoute("r3", "GET", "/c/", "http://c"))

	routes := rt.Routes()
	if len(routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(routes))
	}
}

func TestRoutes_AfterRemove(t *testing.T) {
	rt := New()
	_ = rt.Add(makeRoute("r1", "GET", "/a", "http://a"))
	_ = rt.Add(makeRoute("r2", "POST", "/b", "http://b"))
	_ = rt.Remove("r1")

	routes := rt.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route after remove, got %d", len(routes))
	}
	if routes[0].ID != "r2" {
		t.Fatalf("expected r2 to remain, got %s", routes[0].ID)
	}
}

// --- Concurrency ---

func TestConcurrentAddMatch(t *testing.T) {
	rt := New()
	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			id := "r" + string(rune('0'+i%10))
			_ = rt.Add(makeRoute(id, "GET", "/path/"+id, "http://upstream"))
		}
		close(done)
	}()

	// Reader goroutine
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = rt.Routes()
			}
		}
	}()

	<-done
}
