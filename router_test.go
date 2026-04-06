package protosource

import (
	"context"
	"net/http"
	"testing"
)

func handler(body string) HandlerFunc {
	return func(ctx context.Context, req Request) Response {
		return Response{StatusCode: http.StatusOK, Body: body}
	}
}

func TestRouterExactMatch(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "example/app/sample/v1/create", handler("create"))

	resp := r.Dispatch(context.Background(), "POST", "example/app/sample/v1/create", Request{})
	if resp.StatusCode != http.StatusOK || resp.Body != "create" {
		t.Fatalf("expected 200/create, got %d/%s", resp.StatusCode, resp.Body)
	}
}

func TestRouterLeadingSlash(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "example/app/sample/v1/create", handler("create"))

	resp := r.Dispatch(context.Background(), "POST", "/example/app/sample/v1/create", Request{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRouterParamExtraction(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "example/app/sample/v1/{id}", handler("get"))

	resp := r.Dispatch(context.Background(), "GET", "/example/app/sample/v1/abc-123", Request{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRouterParamMergedIntoRequest(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "example/app/sample/v1/{id}", func(ctx context.Context, req Request) Response {
		return Response{StatusCode: http.StatusOK, Body: req.PathParameters["id"]}
	})

	resp := r.Dispatch(context.Background(), "GET", "/example/app/sample/v1/my-id", Request{})
	if resp.Body != "my-id" {
		t.Fatalf("expected body=my-id, got %s", resp.Body)
	}
}

func TestRouterPreservesExistingPathParams(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "example/app/sample/v1/{id}", func(ctx context.Context, req Request) Response {
		return Response{StatusCode: http.StatusOK, Body: req.PathParameters["id"] + "," + req.PathParameters["extra"]}
	})

	resp := r.Dispatch(context.Background(), "GET", "/example/app/sample/v1/my-id", Request{
		PathParameters: map[string]string{"extra": "val"},
	})
	if resp.Body != "my-id,val" {
		t.Fatalf("expected body=my-id,val, got %s", resp.Body)
	}
}

func TestRouter404(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "example/app/sample/v1/create", handler("create"))

	resp := r.Dispatch(context.Background(), "POST", "/no/such/path", Request{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRouter405(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "example/app/sample/v1/create", handler("create"))

	resp := r.Dispatch(context.Background(), "GET", "/example/app/sample/v1/create", Request{})
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestRouterMultipleRoutes(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "a/v1/create", handler("a-create"))
	r.Handle("POST", "b/v1/create", handler("b-create"))
	r.Handle("GET", "a/v1/{id}", handler("a-get"))
	r.Handle("GET", "a/v1/{id}/history", handler("a-history"))

	tests := []struct {
		method, path, wantBody string
		wantStatus             int
	}{
		{"POST", "/a/v1/create", "a-create", 200},
		{"POST", "/b/v1/create", "b-create", 200},
		{"GET", "/a/v1/some-id", "a-get", 200},
		{"GET", "/a/v1/some-id/history", "a-history", 200},
	}
	for _, tt := range tests {
		resp := r.Dispatch(context.Background(), tt.method, tt.path, Request{})
		if resp.StatusCode != tt.wantStatus || resp.Body != tt.wantBody {
			t.Errorf("%s %s: want %d/%s, got %d/%s", tt.method, tt.path, tt.wantStatus, tt.wantBody, resp.StatusCode, resp.Body)
		}
	}
}

type stubRegistrar struct {
	method, pattern, body string
}

func (s *stubRegistrar) RegisterRoutes(r *Router) {
	r.Handle(s.method, s.pattern, handler(s.body))
}

func TestNewRouterWithRegistrars(t *testing.T) {
	r := NewRouter(
		&stubRegistrar{"POST", "a/v1/create", "a-create"},
		&stubRegistrar{"POST", "b/v1/create", "b-create"},
	)

	resp := r.Dispatch(context.Background(), "POST", "/a/v1/create", Request{})
	if resp.StatusCode != 200 || resp.Body != "a-create" {
		t.Fatalf("want 200/a-create, got %d/%s", resp.StatusCode, resp.Body)
	}
	resp = r.Dispatch(context.Background(), "POST", "/b/v1/create", Request{})
	if resp.StatusCode != 200 || resp.Body != "b-create" {
		t.Fatalf("want 200/b-create, got %d/%s", resp.StatusCode, resp.Body)
	}
}

func TestRouterDoubleSlash(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "a/b/c", handler("abc"))

	// Double slashes in the request path should still match
	resp := r.Dispatch(context.Background(), "GET", "/a//b/c", Request{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with double slash, got %d", resp.StatusCode)
	}
}

func TestRouterCORSPreflight(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "a/v1/create", handler("create"))
	r.SetCORS(CORSConfig{
		AllowOrigin:  "*",
		AllowMethods: "GET,POST,OPTIONS",
		AllowHeaders: "Content-Type,X-Actor",
	})

	resp := r.Dispatch(context.Background(), "OPTIONS", "/a/v1/create", Request{})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if resp.Headers["Access-Control-Allow-Origin"] != "*" {
		t.Fatalf("missing CORS origin header")
	}
	if resp.Headers["Access-Control-Allow-Methods"] != "GET,POST,OPTIONS" {
		t.Fatalf("missing CORS methods header")
	}
	if resp.Headers["Access-Control-Allow-Headers"] != "Content-Type,X-Actor" {
		t.Fatalf("missing CORS headers header")
	}
}

func TestRouterCORSOnResponse(t *testing.T) {
	r := NewRouter()
	r.Handle("POST", "a/v1/create", handler("create"))
	r.SetCORS(CORSConfig{
		AllowOrigin:  "https://example.com",
		AllowMethods: "GET,POST",
		AllowHeaders: "Content-Type",
	})

	resp := r.Dispatch(context.Background(), "POST", "/a/v1/create", Request{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Headers["Access-Control-Allow-Origin"] != "https://example.com" {
		t.Fatalf("expected CORS origin on normal response, got %q", resp.Headers["Access-Control-Allow-Origin"])
	}
}

func TestRouterCORSOn404(t *testing.T) {
	r := NewRouter()
	r.SetCORS(CORSConfig{AllowOrigin: "*", AllowMethods: "GET", AllowHeaders: "Content-Type"})

	resp := r.Dispatch(context.Background(), "GET", "/nope", Request{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if resp.Headers["Access-Control-Allow-Origin"] != "*" {
		t.Fatalf("expected CORS headers on 404")
	}
}

func TestRouterNoCORSByDefault(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "a/v1/x", handler("x"))

	resp := r.Dispatch(context.Background(), "GET", "/a/v1/x", Request{})
	if _, ok := resp.Headers["Access-Control-Allow-Origin"]; ok {
		t.Fatalf("expected no CORS headers when not configured")
	}
}

func TestRouterEmptyPath(t *testing.T) {
	r := NewRouter()
	r.Handle("GET", "foo", handler("foo"))

	resp := r.Dispatch(context.Background(), "GET", "", Request{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
