// Package awslambda provides an adapter that bridges protosource's
// provider-agnostic handlers to AWS API Gateway Lambda proxy integration.
package awslambda

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/funinthecloud/protosource"
)

// ActorExtractor extracts the actor identity from an API Gateway request.
// Return an empty string if no identity can be determined.
type ActorExtractor func(events.APIGatewayProxyRequest) string

// Adapter wraps a protosource.HandlerFunc with AWS API Gateway conversion
// and actor extraction.
type Adapter struct {
	handler   protosource.HandlerFunc
	extractor ActorExtractor
}

// New creates an Adapter that converts API Gateway requests to protosource
// requests, extracts the actor using the provided extractor, and converts
// the protosource response back to an API Gateway response.
func New(handler protosource.HandlerFunc, extractor ActorExtractor) *Adapter {
	return &Adapter{
		handler:   handler,
		extractor: extractor,
	}
}

// Handle is the Lambda entry point. Pass this to lambda.Start().
func (a *Adapter) Handle(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	req := protosource.Request{
		Body:            request.Body,
		PathParameters:  request.PathParameters,
		QueryParameters: request.QueryStringParameters,
		Actor:           a.extractor(request),
	}

	resp := a.handler(ctx, req)

	return events.APIGatewayProxyResponse{
		StatusCode: resp.StatusCode,
		Body:       resp.Body,
		Headers:    resp.Headers,
	}, nil
}

// Wrap is a convenience function that returns the Handle method directly,
// suitable for passing to lambda.Start().
func Wrap(handler protosource.HandlerFunc, extractor ActorExtractor) func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return New(handler, extractor).Handle
}
