package httpclient

import (
	"fmt"
	"strings"

	apierrorv1 "github.com/funinthecloud/protosource/gen/apierror/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// APIError represents an error response from the server. The wire body is an
// apierror.v1.Error (protobuf binary by default, JSON in debug mode); the
// StatusCode comes from the HTTP status line, not the body.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("httpclient: %d %s: %s (%s)", e.StatusCode, e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("httpclient: %d %s: %s", e.StatusCode, e.Code, e.Message)
}

// parseAPIError decodes an error response body into an *APIError. The body is
// an apierror.v1.Error, content-negotiated like every other message; the
// contentType (from the response's Content-Type header) selects protobuf or
// JSON decoding. Bodies that are not a valid Error — e.g. a plaintext 502 from
// a load balancer or an HTML gateway page — fall back to a synthetic UNKNOWN
// error carrying the raw body as the message.
func parseAPIError(statusCode int, contentType string, body []byte) *APIError {
	wire := &apierrorv1.Error{}
	var decodeErr error
	if strings.Contains(contentType, "json") {
		decodeErr = protojson.Unmarshal(body, wire)
	} else {
		decodeErr = proto.Unmarshal(body, wire)
	}
	if decodeErr != nil || wire.GetCode() == "" {
		return &APIError{
			StatusCode: statusCode,
			Code:       "UNKNOWN",
			Message:    string(body),
		}
	}
	return &APIError{
		StatusCode: statusCode,
		Code:       wire.GetCode(),
		Message:    wire.GetMessage(),
		Detail:     wire.GetDetail(),
	}
}
