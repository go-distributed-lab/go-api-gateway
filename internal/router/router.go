package router

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"go-api-gateway/pkg/gateway"
)

// MatchedRoute is returned by the Router when a route is successfully matched.
type MatchedRoute struct {
	Route  gateway.Route
	Params map[string]string
}

// Router matches an incoming HTTP request to a registered Route.
// Safe for concurrent use.
type Router interface {
	Add(route gateway.Route) error
	Remove(id string) error
	Match(r *http.Request) (MatchedRoute, error)
	Routes() []gateway.Route
}

// mapRouter is the default Router implementation.
// Exact matches are O(1); prefix matches are O(n) on the number of prefix routes.
type mapRouter struct {
	mu     sync.RWMutex
	exact  map[string]gateway.Route // key: "METHOD:path" or ":path" for any-method
	prefix []gateway.Route          // sorted longest-path-first
	byID   map[string]gateway.Route // key: route ID — for O(1) removal
}

// New returns a new Router backed by mapRouter.
func New() Router {
	return &mapRouter{
		exact: make(map[string]gateway.Route),
		byID:  make(map[string]gateway.Route),
	}
}

// Add registers a route.
// Returns ErrRouteDuplicate if a route with the same ID already exists,
// or ErrInvalidRoute if the route is missing a path or upstream.
func (rt *mapRouter) Add(route gateway.Route) error {
	if route.ID == "" || route.Path == "" || route.Upstream == "" {
		return gateway.ErrInvalidRoute
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if _, exists := rt.byID[route.ID]; exists {
		return gateway.ErrRouteDuplicate
	}

	rt.byID[route.ID] = route

	if isPrefix(route.Path) {
		rt.prefix = append(rt.prefix, route)
		sortPrefixes(rt.prefix)
	} else {
		key := exactKey(route.Method, route.Path)
		rt.exact[key] = route
	}

	return nil
}

// Remove removes a route by ID.
// Returns ErrRouteNotFound if the ID is not registered.
func (rt *mapRouter) Remove(id string) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	route, exists := rt.byID[id]
	if !exists {
		return gateway.ErrRouteNotFound
	}

	delete(rt.byID, id)

	if isPrefix(route.Path) {
		rt.prefix = removeFromSlice(rt.prefix, id)
	} else {
		key := exactKey(route.Method, route.Path)
		delete(rt.exact, key)
	}

	return nil
}

// Match returns the best matching route for the given request.
// Exact match takes priority over prefix match.
// Among prefix matches, the longest path wins.
// Returns ErrRouteNotFound if nothing matches.
func (rt *mapRouter) Match(r *http.Request) (MatchedRoute, error) {
	method := r.Method
	path := r.URL.Path

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// 1. Exact match — method-specific first, then any-method.
	if route, ok := rt.exact[exactKey(method, path)]; ok {
		return MatchedRoute{Route: route}, nil
	}
	if route, ok := rt.exact[exactKey("", path)]; ok {
		return MatchedRoute{Route: route}, nil
	}

	// 2. Prefix match — slice is sorted longest-first so first hit wins.
	for _, route := range rt.prefix {
		if !strings.HasPrefix(path, route.Path) {
			continue
		}
		if route.Method != "" && route.Method != method {
			continue
		}
		return MatchedRoute{Route: route}, nil
	}

	return MatchedRoute{}, gateway.ErrRouteNotFound
}

// Routes returns a snapshot of all registered routes.
func (rt *mapRouter) Routes() []gateway.Route {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	out := make([]gateway.Route, 0, len(rt.byID))
	for _, r := range rt.byID {
		out = append(out, r)
	}
	return out
}

// --- helpers ---

// isPrefix reports whether path is a prefix pattern (ends with /).
func isPrefix(path string) bool {
	return strings.HasSuffix(path, "/")
}

// exactKey builds the map key for an exact route.
// An empty method means "match any method".
func exactKey(method, path string) string {
	if method == "" {
		return ":" + path
	}
	return method + ":" + path
}

// sortPrefixes sorts routes longest-path-first so the most specific prefix wins.
func sortPrefixes(routes []gateway.Route) {
	sort.Slice(routes, func(i, j int) bool {
		return len(routes[i].Path) > len(routes[j].Path)
	})
}

// removeFromSlice returns a new slice with the route matching id removed.
func removeFromSlice(routes []gateway.Route, id string) []gateway.Route {
	out := routes[:0]
	for _, r := range routes {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}
