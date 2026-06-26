package awslambda

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/funinthecloud/protosource"
)

func TestAdapter_Handle(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
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

	adapter := New(handler)

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

	adapter := New(handler)

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

func TestAdapter_Handle_Base64DecodeFailure(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		t.Fatal("handler should not be called on decode failure")
		return protosource.Response{}
	}

	adapter := New(handler)

	request := events.APIGatewayProxyRequest{
		Body:            "not-valid-base64!!!",
		IsBase64Encoded: true,
	}

	resp, err := adapter.Handle(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
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

	adapter := New(handler)

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

func TestAdapter_Handle_MultipleCookies(t *testing.T) {
	// REST API Gateway collapses Headers to one value per key, so multiple
	// Set-Cookie headers (set one + clear another) must come through
	// MultiValueHeaders. The single-value Headers map is left untouched.
	handler := func(_ context.Context, _ protosource.Request) protosource.Response {
		return protosource.Response{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Cookies: []*http.Cookie{
				{Name: "shadow", Value: "session-token", Path: "/", HttpOnly: true},
				{Name: "shadow_oauth_state", Value: "", Path: "/oauth/callback", MaxAge: -1},
			},
		}
	}

	resp, err := New(handler).Handle(context.Background(), events.APIGatewayProxyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	setCookies := resp.MultiValueHeaders["Set-Cookie"]
	if len(setCookies) != 2 {
		t.Fatalf("expected 2 Set-Cookie values, got %d: %v", len(setCookies), setCookies)
	}
	if !strings.Contains(setCookies[0], "shadow=session-token") {
		t.Errorf("expected first cookie to set shadow session, got %q", setCookies[0])
	}
	if !strings.Contains(setCookies[1], "shadow_oauth_state=") ||
		!strings.Contains(setCookies[1], "Max-Age=0") {
		t.Errorf("expected second cookie to clear shadow_oauth_state, got %q", setCookies[1])
	}
	// Single-value Headers remain intact and untouched by cookie handling.
	if resp.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type header preserved, got %q", resp.Headers["Content-Type"])
	}
}

func TestAdapter_Handle_NoCookiesLeavesMultiValueNil(t *testing.T) {
	// Backward-compat: a response with no Cookies must not populate
	// MultiValueHeaders at all.
	handler := func(_ context.Context, _ protosource.Request) protosource.Response {
		return protosource.Response{StatusCode: http.StatusOK, Body: "ok"}
	}
	resp, err := New(handler).Handle(context.Background(), events.APIGatewayProxyRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.MultiValueHeaders != nil {
		t.Errorf("expected nil MultiValueHeaders, got %v", resp.MultiValueHeaders)
	}
}

func TestWrap(t *testing.T) {
	handler := func(ctx context.Context, req protosource.Request) protosource.Response {
		return protosource.Response{StatusCode: http.StatusOK, Body: "ok"}
	}

	fn := Wrap(handler)
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

	fn := WrapRouter(router)

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

// TestDecodeRequest_LowercasesHeaderKeys locks in the contract that all header
// keys are normalized to lowercase before dispatch. API Gateway preserves
// whatever case the client sent; downstream code must be able to rely on a
// single canonical form.
func TestDecodeRequest_LowercasesHeaderKeys(t *testing.T) {
	in := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Path:       "/x",
		Headers: map[string]string{
			"Origin":       "https://example.com",
			"Content-Type": "application/json",
			"X-Custom":     "v",
		},
	}
	req, err := decodeRequest(in)
	if err != nil {
		t.Fatalf("decodeRequest: %v", err)
	}
	for _, k := range []string{"origin", "content-type", "x-custom"} {
		if _, ok := req.Headers[k]; !ok {
			t.Errorf("expected lowercased key %q in decoded headers, got %v", k, req.Headers)
		}
	}
	for _, k := range []string{"Origin", "Content-Type", "X-Custom"} {
		if _, ok := req.Headers[k]; ok {
			t.Errorf("did not expect original-case key %q to remain", k)
		}
	}
}
