// Package httpstandard provides an adapter that bridges protosource's
// provider-agnostic handlers to standard net/http. Suitable for local
// development, container deployments, DigitalOcean App Platform, or
// any environment that speaks HTTP.
package httpstandard

import (
	"io"
	"net/http"

	"github.com/funinthecloud/protosource"
)

// ActorExtractor extracts the actor identity from an HTTP request.
// Return an empty string if no identity can be determined.
type ActorExtractor func(*http.Request) string

// Wrap returns an http.HandlerFunc that converts between net/http and
// protosource's provider-agnostic types. The extractor populates the
// actor identity on each request.
func Wrap(handler protosource.HandlerFunc, extractor ActorExtractor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"failed to read request body"}`)
			return
		}

		pathParams := make(map[string]string)
		if id := r.PathValue("id"); id != "" {
			pathParams["id"] = id
		}

		queryParams := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				queryParams[k] = v[0]
			}
		}

		headers := make(map[string]string)
		for k := range r.Header {
			headers[k] = r.Header.Get(k)
		}

		req := protosource.Request{
			Body:            string(body),
			PathParameters:  pathParams,
			QueryParameters: queryParams,
			Headers:         headers,
			Actor:           extractor(r),
		}

		resp := handler(r.Context(), req)

		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.WriteString(w, resp.Body)
	}
}

// WrapRouter returns an http.Handler that dispatches to the router based on
// the request's HTTP method and URL path.
func WrapRouter(router *protosource.Router, extractor ActorExtractor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"failed to read request body"}`)
			return
		}

		queryParams := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				queryParams[k] = v[0]
			}
		}

		headers := make(map[string]string)
		for k := range r.Header {
			headers[k] = r.Header.Get(k)
		}

		req := protosource.Request{
			Body:            string(body),
			QueryParameters: queryParams,
			Headers:         headers,
			Actor:           extractor(r),
		}

		resp := router.Dispatch(r.Context(), r.Method, r.URL.Path, req)

		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.WriteString(w, resp.Body)
	})
}

// BearerTokenExtractor returns an ActorExtractor that reads the
// Authorization header and returns the bearer token value as the actor.
// This is a simple extractor for development/testing; in production,
// you'd typically validate the JWT and extract claims.
func BearerTokenExtractor(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

// HeaderExtractor returns an ActorExtractor that reads the actor
// identity from a specific HTTP header. Useful when a reverse proxy
// or API gateway sets a header like X-User-Id after authentication.
func HeaderExtractor(header string) ActorExtractor {
	return func(r *http.Request) string {
		return r.Header.Get(header)
	}
}
