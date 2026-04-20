package protosource

import (
	"context"
	"net/http"
	"strings"
)

// CORSConfig configures Cross-Origin Resource Sharing headers on the router.
// When set, Dispatch automatically handles OPTIONS preflight requests and
// injects CORS headers into every response.
type CORSConfig struct {
	AllowOrigin      string // e.g. "*" or "https://example.com"
	AllowMethods     string // e.g. "GET,POST,PUT,DELETE,OPTIONS"
	AllowHeaders     string // e.g. "Content-Type,X-Actor"
	AllowCredentials bool   // when true, sets Access-Control-Allow-Credentials: true
}

// Router maps HTTP method + path patterns to HandlerFunc handlers.
// Path patterns support {param} segments for parameter extraction.
type Router struct {
	routes []route
	cors   *CORSConfig
}

type route struct {
	method   string
	segments []string
	handler  HandlerFunc
}

// RouteRegistrar is implemented by types that register routes on a Router.
// Generated Handler types satisfy this interface.
type RouteRegistrar interface {
	RegisterRoutes(router *Router)
}

// NewRouter creates a new Router. If registrars are provided, their routes
// are registered immediately.
func NewRouter(registrars ...RouteRegistrar) *Router {
	r := &Router{}
	for _, reg := range registrars {
		reg.RegisterRoutes(r)
	}
	return r
}

// SetCORS enables CORS handling on the router. When set, Dispatch responds
// to OPTIONS requests with a 204 preflight response and adds CORS headers
// to all responses.
func (r *Router) SetCORS(cfg CORSConfig) {
	r.cors = &cfg
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
	// Handle CORS preflight.
	if r.cors != nil && method == http.MethodOptions {
		resp := Response{
			StatusCode: http.StatusNoContent,
		}
		r.applyCORS(&resp)
		return resp
	}

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
		resp := rt.handler(ctx, request)
		r.applyCORS(&resp)
		return resp
	}

	if methodMismatch {
		resp := Response{
			StatusCode: http.StatusMethodNotAllowed,
			Body:       `{"error":"method not allowed"}`,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}
		r.applyCORS(&resp)
		return resp
	}

	resp := Response{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"not found"}`,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
	r.applyCORS(&resp)
	return resp
}

// applyCORS injects CORS headers into the response if CORS is configured.
func (r *Router) applyCORS(resp *Response) {
	if r.cors == nil {
		return
	}
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	resp.Headers["Access-Control-Allow-Origin"] = r.cors.AllowOrigin
	resp.Headers["Access-Control-Allow-Methods"] = r.cors.AllowMethods
	resp.Headers["Access-Control-Allow-Headers"] = r.cors.AllowHeaders
	if r.cors.AllowCredentials {
		resp.Headers["Access-Control-Allow-Credentials"] = "true"
	}
}

// splitPath splits a URL path into non-empty segments.
func splitPath(path string) []string {
	parts := strings.Split(path, "/")
	segments := parts[:0]
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	if len(segments) == 0 {
		return nil
	}
	return segments
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
