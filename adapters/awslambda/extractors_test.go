package awslambda

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestCognitoExtractor(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub": "user-123",
				},
			},
		},
	}
	if got := CognitoExtractor(request); got != "user-123" {
		t.Errorf("expected 'user-123', got %q", got)
	}
}

func TestCognitoExtractor_NoClaims(t *testing.T) {
	request := events.APIGatewayProxyRequest{}
	if got := CognitoExtractor(request); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestOktaExtractor_Uid(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"uid": "okta-user-456",
					"sub": "fallback-sub",
				},
			},
		},
	}
	if got := OktaExtractor(request); got != "okta-user-456" {
		t.Errorf("expected 'okta-user-456', got %q", got)
	}
}

func TestOktaExtractor_FallbackToSub(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub": "okta-sub-789",
				},
			},
		},
	}
	if got := OktaExtractor(request); got != "okta-sub-789" {
		t.Errorf("expected 'okta-sub-789', got %q", got)
	}
}

func TestIAMExtractor(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789:user/admin",
			},
		},
	}
	if got := IAMExtractor(request); got != "arn:aws:iam::123456789:user/admin" {
		t.Errorf("expected IAM ARN, got %q", got)
	}
}

func TestCustomAuthExtractor(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"principalId": "custom-principal",
			},
		},
	}
	if got := CustomAuthExtractor(request); got != "custom-principal" {
		t.Errorf("expected 'custom-principal', got %q", got)
	}
}

func TestCustomAuthExtractor_Missing(t *testing.T) {
	request := events.APIGatewayProxyRequest{}
	if got := CustomAuthExtractor(request); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestChain_FirstMatch(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub": "cognito-user",
				},
			},
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123:user/admin",
			},
		},
	}
	extractor := Chain(CognitoExtractor, IAMExtractor)
	if got := extractor(request); got != "cognito-user" {
		t.Errorf("expected 'cognito-user' (first match), got %q", got)
	}
}

func TestChain_Fallback(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123:user/admin",
			},
		},
	}
	extractor := Chain(CognitoExtractor, IAMExtractor)
	if got := extractor(request); got != "arn:aws:iam::123:user/admin" {
		t.Errorf("expected IAM ARN (fallback), got %q", got)
	}
}

func TestChain_NoMatch(t *testing.T) {
	request := events.APIGatewayProxyRequest{}
	extractor := Chain(CognitoExtractor, IAMExtractor)
	if got := extractor(request); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
