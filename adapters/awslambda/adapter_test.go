package awslambda

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/funinthecloud/protosource"
)

func TestAdapter_Handle(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		if req.Actor != "test-actor" {
			t.Errorf("expected actor 'test-actor', got %q", req.Actor)
		}
		if req.Body != `{"id":"123"}` {
			t.Errorf("expected body, got %q", req.Body)
		}
		if req.PathParameters["id"] != "123" {
			t.Errorf("expected path param id=123, got %q", req.PathParameters["id"])
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

	extractor := func(request events.APIGatewayProxyRequest) string {
		return "test-actor"
	}

	adapter := New(handler, extractor)

	request := events.APIGatewayProxyRequest{
		Body:                  `{"id":"123"}`,
		PathParameters:        map[string]string{"id": "123"},
		QueryStringParameters: map[string]string{"filter": "active"},
	}

	resp, err := adapter.Handle(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Body != `{"ok":true}` {
		t.Errorf("expected body, got %q", resp.Body)
	}
}

func TestWrap(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		return protosource.Response{StatusCode: http.StatusOK, Body: "ok"}
	}
	extractor := func(request events.APIGatewayProxyRequest) string {
		return "actor"
	}

	fn := Wrap(handler, extractor)
	resp, err := fn(context.Background(), events.APIGatewayProxyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
