package protosource

import (
	"context"
	"net/http"
	"strings"
)

// Router maps HTTP method + path patterns to HandlerFunc handlers.
// Path patterns support {param} segments for parameter extraction.
type Router struct {
	routes []route
}

type route struct {
	method   string
	segments []string
	handler  HandlerFunc
}

// NewRouter creates a new Router.
func NewRouter() *Router {
	return &Router{}
}

// Handle registers a handler for the given HTTP method and path pattern.
// Patterns use {name} for path parameters (e.g., "example/app/sample/v1/{id}").
func (r *Router) Handle(method, pattern string, handler HandlerFunc) {
	r.routes = append(r.routes, route{
		method:   method,
		segments: splitPath(pattern),
		handler:  handler,
	})
}

// Dispatch finds a matching route and invokes its handler. Path parameters
// extracted from the pattern are merged into request.PathParameters.
// Returns 404 for no path match, 405 for path match with wrong method.
func (r *Router) Dispatch(ctx context.Context, method, path string, request Request) Response {
	pathSegs := splitPath(path)

	methodMismatch := false
	for _, rt := range r.routes {
		params, ok := matchSegments(rt.segments, pathSegs)
		if !ok {
			continue
		}
		if rt.method != method {
			methodMismatch = true
			continue
		}
		if request.PathParameters == nil {
			request.PathParameters = make(map[string]string)
		}
		for k, v := range params {
			request.PathParameters[k] = v
		}
		return rt.handler(ctx, request)
	}

	if methodMismatch {
		return Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       `{"error":"method not allowed"}`,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}
	}

	return Response{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"not found"}`,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// splitPath splits a URL path into non-empty segments.
func splitPath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// matchSegments checks whether pathSegs matches the pattern segments.
// Returns extracted parameters and true on match.
func matchSegments(pattern, path []string) (map[string]string, bool) {
	if len(pattern) != len(path) {
		return nil, false
	}
	var params map[string]string
	for i, seg := range pattern {
		if len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			if params == nil {
				params = make(map[string]string)
			}
			params[seg[1:len(seg)-1]] = path[i]
			continue
		}
		if seg != path[i] {
			return nil, false
		}
	}
	return params, true
}
