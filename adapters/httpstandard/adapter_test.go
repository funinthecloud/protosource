package httpstandard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/funinthecloud/protosource"
)

func TestWrap(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		if req.Actor != "user-123" {
			t.Errorf("expected actor 'user-123', got %q", req.Actor)
		}
		if req.Body != `{"id":"agg-1"}` {
			t.Errorf("expected body, got %q", req.Body)
		}
		if req.QueryParameters["filter"] != "active" {
			t.Errorf("expected query param filter=active, got %q", req.QueryParameters["filter"])
		}
		return protosource.Response{
			StatusCode: http.StatusOK,
			Body:       `{"ok":true}`,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}
	}

	extractor := HeaderExtractor("X-User-Id")
	wrapped := Wrap(handler, extractor)

	body := strings.NewReader(`{"id":"agg-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/create?filter=active", body)
	req.Header.Set("X-User-Id", "user-123")

	rec := httptest.NewRecorder()
	wrapped(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("expected body, got %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestBearerTokenExtractor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-token-123")

	if got := BearerTokenExtractor(req); got != "my-token-123" {
		t.Errorf("expected 'my-token-123', got %q", got)
	}
}

func TestBearerTokenExtractor_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := BearerTokenExtractor(req); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBearerTokenExtractor_NotBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc123")
	if got := BearerTokenExtractor(req); got != "" {
		t.Errorf("expected empty string for Basic auth, got %q", got)
	}
}

func TestHeaderExtractor(t *testing.T) {
	extractor := HeaderExtractor("X-Custom-Actor")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Custom-Actor", "custom-user")

	if got := extractor(req); got != "custom-user" {
		t.Errorf("expected 'custom-user', got %q", got)
	}
}

func TestHeaderExtractor_Missing(t *testing.T) {
	extractor := HeaderExtractor("X-Custom-Actor")
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if got := extractor(req); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
