package router

import (
	"net/http"

	"go-api-gateway/pkg/gateway"
)

// MatchedRoute is returned by the Router when a route is successfully matched.
// It carries the resolved Route plus any path parameters extracted from the URL.
type MatchedRoute struct {
	Route  gateway.Route
	Params map[string]string // path parameters, e.g. {"id": "42"}
}

// Router matches an incoming HTTP request to a registered Route.
// Implementations must be safe for concurrent use.
type Router interface {
	// Add registers a route. Returns an error if the route is invalid or duplicate.
	Add(route gateway.Route) error

	// Remove removes a route by ID. Returns gateway.ErrRouteNotFound if absent.
	Remove(id string) error

	// Match returns the best matching route for the given request.
	// Returns gateway.ErrRouteNotFound if no route matches.
	Match(r *http.Request) (MatchedRoute, error)

	// Routes returns a snapshot of all registered routes.
	Routes() []gateway.Route
}
