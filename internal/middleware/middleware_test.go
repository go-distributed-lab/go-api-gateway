package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-api-gateway/internal/logger"
)

// --- helpers ---

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func panicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})
}



// --- Chain ---

func TestChain_SingleMiddleware(t *testing.T) {
	order := []string{}
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw")
			next.ServeHTTP(w, r)
		})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	h := Chain(handler, mw)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(order) != 2 || order[0] != "mw" || order[1] != "handler" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestChain_OrderIsOuterToInner(t *testing.T) {
	order := []string{}
	make_mw := func(name string) Func {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-in")
				next.ServeHTTP(w, r)
				order = append(order, name+"-out")
			})
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	h := Chain(handler, make_mw("A"), make_mw("B"), make_mw("C"))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	want := []string{"A-in", "B-in", "C-in", "handler", "C-out", "B-out", "A-out"}
	for i, v := range want {
		if order[i] != v {
			t.Fatalf("position %d: want %q got %q (full: %v)", i, v, order[i], order)
		}
	}
}

// --- responseRecorder ---

func TestResponseRecorder_DefaultStatus(t *testing.T) {
	rec := newResponseRecorder(httptest.NewRecorder())
	if rec.Status() != http.StatusOK {
		t.Fatalf("expected default 200, got %d", rec.Status())
	}
}

func TestResponseRecorder_CapturesStatus(t *testing.T) {
	rec := newResponseRecorder(httptest.NewRecorder())
	rec.WriteHeader(http.StatusCreated)
	if rec.Status() != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Status())
	}
}

func TestResponseRecorder_CapturesBytes(t *testing.T) {
	rec := newResponseRecorder(httptest.NewRecorder())
	_, _ = rec.Write([]byte("hello"))
	if rec.BytesWritten() != 5 {
		t.Fatalf("expected 5 bytes, got %d", rec.BytesWritten())
	}
}

func TestResponseRecorder_WriteHeaderOnce(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := newResponseRecorder(inner)
	rec.WriteHeader(http.StatusAccepted)
	rec.WriteHeader(http.StatusTeapot) // should be ignored
	if rec.Status() != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Status())
	}
}

// --- Recovery ---

func TestRecovery_NoPanic(t *testing.T) {
	h := Chain(okHandler(), Recovery(nil))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRecovery_CatchesPanic(t *testing.T) {
	var caughtErr any
	h := Chain(panicHandler(), Recovery(func(err any, stack []byte) {
		caughtErr = err
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if caughtErr == nil {
		t.Fatal("expected panic to be captured")
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("expected error body, got %q", rec.Body.String())
	}
}

func TestRecovery_NilCallbackSafe(t *testing.T) {
	h := Chain(panicHandler(), Recovery(nil))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	// Should not panic itself.
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- Logger ---

func TestLogger_LogsRequest(t *testing.T) {
	var buf strings.Builder
	log := logger.New(&buf)

	h := Chain(okHandler(), Logger(log))
	req := httptest.NewRequest("GET", "/test-path", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "method=GET") {
		t.Errorf("expected method=GET in log, got: %s", out)
	}
	if !strings.Contains(out, "path=/test-path") {
		t.Errorf("expected path=/test-path in log, got: %s", out)
	}
	if !strings.Contains(out, "status=200") {
		t.Errorf("expected status=200 in log, got: %s", out)
	}
}

func TestLogger_Discard(t *testing.T) {
	log := logger.New(io.Discard)
	h := Chain(okHandler(), Logger(log))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	// Should not panic and request should still succeed.
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- Transform: request headers ---

func TestTransform_AddRequestHeader(t *testing.T) {
	cfg := TransformConfig{
		AddRequestHeaders: map[string]string{"X-Service": "gateway"},
	}
	var seen string
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("X-Service")
			w.WriteHeader(http.StatusOK)
		}),
		Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != "gateway" {
		t.Fatalf("expected X-Service=gateway, got %q", seen)
	}
}

func TestTransform_RemoveRequestHeader(t *testing.T) {
	cfg := TransformConfig{
		RemoveRequestHeaders: []string{"X-Secret"},
	}
	var seen string
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("X-Secret")
			w.WriteHeader(http.StatusOK)
		}),
		Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Secret", "should-be-removed")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != "" {
		t.Fatalf("expected X-Secret to be removed, got %q", seen)
	}
}

// --- Transform: response headers ---

func TestTransform_AddResponseHeader(t *testing.T) {
	cfg := TransformConfig{
		AddResponseHeaders: map[string]string{"X-Powered-By": "go-api-gateway"},
	}
	h := Chain(okHandler(), Transform(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Powered-By") != "go-api-gateway" {
		t.Fatalf("expected X-Powered-By header, got %q", rec.Header().Get("X-Powered-By"))
	}
}

func TestTransform_RemoveResponseHeader(t *testing.T) {
	cfg := TransformConfig{
		RemoveResponseHeaders: []string{"X-Internal"},
	}
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Internal", "secret")
			w.WriteHeader(http.StatusOK)
		}),
		Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Internal") != "" {
		t.Fatalf("expected X-Internal to be removed, got %q", rec.Header().Get("X-Internal"))
	}
}

// --- Transform: path rewrite ---

func TestTransform_RewritePath(t *testing.T) {
	cfg := TransformConfig{RewritePath: "/api/v1|/v1"}
	var seenPath string
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}),
		Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seenPath != "/v1/users" {
		t.Fatalf("expected /v1/users, got %q", seenPath)
	}
}

func TestTransform_RewritePath_NoMatch(t *testing.T) {
	cfg := TransformConfig{RewritePath: "/api/v1|/v1"}
	var seenPath string
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}),
		Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/other/path", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seenPath != "/other/path" {
		t.Fatalf("expected path unchanged, got %q", seenPath)
	}
}

func TestRewritePath(t *testing.T) {
	cases := []struct {
		path, rule, want string
	}{
		{"/api/v1/users", "/api/v1|/v1", "/v1/users"},
		{"/other", "/api/v1|/v1", "/other"},
		{"/api/v1", "/api/v1|", ""},
		{"/x", "malformed", "/x"},
		{"/x", "", "/x"},
	}
	for _, c := range cases {
		got := rewritePath(c.path, c.rule)
		if got != c.want {
			t.Errorf("rewritePath(%q, %q) = %q, want %q", c.path, c.rule, got, c.want)
		}
	}
}
