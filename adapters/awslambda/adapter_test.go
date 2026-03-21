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

func TestAdapter_Handle_Base64Body(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		if req.Body != "decoded-binary" {
			t.Errorf("expected decoded body, got %q", req.Body)
		}
		return protosource.Response{StatusCode: http.StatusOK, Body: "ok"}
	}

	extractor := func(request events.APIGatewayProxyRequest) string { return "actor" }
	adapter := New(handler, extractor)

	// "decoded-binary" base64-encoded
	request := events.APIGatewayProxyRequest{
		Body:            "ZGVjb2RlZC1iaW5hcnk=",
		IsBase64Encoded: true,
	}

	resp, err := adapter.Handle(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdapter_Handle_BinaryResponse(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		return protosource.Response{
			StatusCode: http.StatusOK,
			Body:       "binary-data",
			Headers:    map[string]string{"Content-Type": "application/protobuf"},
		}
	}

	extractor := func(request events.APIGatewayProxyRequest) string { return "actor" }
	adapter := New(handler, extractor)

	resp, err := adapter.Handle(context.Background(), events.APIGatewayProxyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsBase64Encoded {
		t.Error("expected IsBase64Encoded=true for protobuf response")
	}
	if resp.Body != "YmluYXJ5LWRhdGE=" {
		t.Errorf("expected base64-encoded body, got %q", resp.Body)
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

func TestWrapRouter(t *testing.T) {
	router := protosource.NewRouter()
	router.Handle("GET", "sample/v1/{id}", func(ctx context.Context, req protosource.Request) protosource.Response {
		return protosource.Response{
			StatusCode: http.StatusOK,
			Body:       req.PathParameters["id"],
			Headers:    map[string]string{"Content-Type": "application/json"},
		}
	})
	router.Handle("POST", "sample/v1/create", func(ctx context.Context, req protosource.Request) protosource.Response {
		return protosource.Response{
			StatusCode: http.StatusOK,
			Body:       req.Body,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}
	})

	extractor := func(request events.APIGatewayProxyRequest) string { return "actor" }
	fn := WrapRouter(router, extractor)

	t.Run("GET with param", func(t *testing.T) {
		resp, err := fn(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			Path:       "/sample/v1/abc-123",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if resp.Body != "abc-123" {
			t.Errorf("expected body=abc-123, got %q", resp.Body)
		}
	})

	t.Run("POST command", func(t *testing.T) {
		resp, err := fn(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			Path:       "/sample/v1/create",
			Body:       `{"id":"x"}`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if resp.Body != `{"id":"x"}` {
			t.Errorf("expected body, got %q", resp.Body)
		}
	})

	t.Run("404 no match", func(t *testing.T) {
		resp, err := fn(context.Background(), events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			Path:       "/no/such/path",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}
