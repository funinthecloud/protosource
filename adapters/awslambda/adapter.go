// Package awslambda provides an adapter that bridges protosource's
// provider-agnostic handlers to AWS API Gateway Lambda proxy integration.
package awslambda

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/funinthecloud/protosource"
)

// Adapter wraps a protosource.HandlerFunc with AWS API Gateway conversion
// and actor extraction.
type Adapter struct {
	handler protosource.HandlerFunc
}

// New creates an Adapter that converts API Gateway requests to protosource
// requests, extracts the actor using the provided extractor, and converts
// the protosource response back to an API Gateway response.
func New(handler protosource.HandlerFunc) *Adapter {
	return &Adapter{
		handler: handler,
	}
}

// Handle is the Lambda entry point. Pass this to lambda.Start().
func (a *Adapter) Handle(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	req, err := decodeRequest(request)
	if err != nil {
		return encodeResponse(protosource.Response{
			StatusCode: 400,
			Body:       `{"error":"failed to decode base64 request body"}`,
			Headers:    map[string]string{"Content-Type": "application/json"},
		}), nil
	}
	resp := a.handler(ctx, req)
	return encodeResponse(resp), nil
}

// Wrap is a convenience function that returns the Handle method directly,
// suitable for passing to lambda.Start().
func Wrap(handler protosource.HandlerFunc) func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return New(handler).Handle
}

// WrapRouter returns a Lambda handler that dispatches to the router based on
// the request's HTTP method and path. Suitable for passing to lambda.Start().
func WrapRouter(router *protosource.Router) func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return func(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		req, err := decodeRequest(request)
		if err != nil {
			return encodeResponse(protosource.Response{
				StatusCode: 400,
				Body:       `{"error":"failed to decode base64 request body"}`,
				Headers:    map[string]string{"Content-Type": "application/json"},
			}), nil
		}
		resp := router.Dispatch(ctx, request.HTTPMethod, request.Path, req)
		return encodeResponse(resp), nil
	}
}

// decodeRequest converts an API Gateway request to a protosource Request,
// decoding base64 bodies when IsBase64Encoded is set. Returns an error if
// base64 decoding fails.
func decodeRequest(request events.APIGatewayProxyRequest) (protosource.Request, error) {
	body := request.Body
	if request.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return protosource.Request{}, err
		}
		body = string(decoded)
	}
	return protosource.Request{
		Body:            body,
		PathParameters:  request.PathParameters,
		QueryParameters: request.QueryStringParameters,
		Headers:         lowercaseHeaders(request.Headers),
	}, nil
}

// lowercaseHeaders normalizes header map keys to lowercase. HTTP header names
// are case-insensitive on the wire (RFC 7230) and required to be lowercase in
// HTTP/2. API Gateway preserves whatever case the client/edge sent, so we
// normalize here to give downstream code a single canonical form to look up.
func lowercaseHeaders(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}

// encodeResponse converts a protosource Response to an API Gateway response,
// base64-encoding the body for binary content types.
func encodeResponse(resp protosource.Response) events.APIGatewayProxyResponse {
	ct := resp.Headers["Content-Type"]
	isBinary := ct == "application/protobuf" || ct == "application/octet-stream"

	apiResp := events.APIGatewayProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
	}
	if isBinary {
		apiResp.Body = base64.StdEncoding.EncodeToString([]byte(resp.Body))
		apiResp.IsBase64Encoded = true
	} else {
		apiResp.Body = resp.Body
	}
	return apiResp
}
