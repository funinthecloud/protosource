package awslambda

import (
	"github.com/aws/aws-lambda-go/events"
)

// CognitoExtractor extracts the actor from a Cognito User Pool JWT authorizer.
// It reads the "sub" claim from the authorizer context.
func CognitoExtractor(request events.APIGatewayProxyRequest) string {
	return claimString(request, "sub")
}

// Auth0Extractor extracts the actor from an Auth0 JWT authorizer.
// Auth0 uses the standard "sub" claim in the JWT, which API Gateway
// passes through the authorizer context.
func Auth0Extractor(request events.APIGatewayProxyRequest) string {
	return claimString(request, "sub")
}

// OktaExtractor extracts the actor from an Okta JWT authorizer.
// Okta uses the "uid" claim for the user identifier.
func OktaExtractor(request events.APIGatewayProxyRequest) string {
	if uid := claimString(request, "uid"); uid != "" {
		return uid
	}
	// Okta also supports "sub" as a fallback.
	return claimString(request, "sub")
}

// IAMExtractor extracts the actor from IAM authorization.
// It returns the caller's ARN from the request identity.
func IAMExtractor(request events.APIGatewayProxyRequest) string {
	return request.RequestContext.Identity.UserArn
}

// CustomAuthExtractor extracts the actor from a custom/Lambda authorizer.
// It reads the "principalId" field from the authorizer context.
func CustomAuthExtractor(request events.APIGatewayProxyRequest) string {
	if principalID, ok := request.RequestContext.Authorizer["principalId"]; ok {
		if pid, ok := principalID.(string); ok {
			return pid
		}
	}
	return ""
}

// Chain returns an ActorExtractor that tries each extractor in order,
// returning the first non-empty result. This is useful when your API
// supports multiple authentication methods (e.g., Cognito for end users,
// IAM for service-to-service calls).
func Chain(extractors ...ActorExtractor) ActorExtractor {
	return func(request events.APIGatewayProxyRequest) string {
		for _, ext := range extractors {
			if actor := ext(request); actor != "" {
				return actor
			}
		}
		return ""
	}
}

// claimString extracts a string claim from the authorizer "claims" map.
func claimString(request events.APIGatewayProxyRequest, key string) string {
	claims, ok := request.RequestContext.Authorizer["claims"]
	if !ok {
		return ""
	}
	claimsMap, ok := claims.(map[string]interface{})
	if !ok {
		return ""
	}
	val, ok := claimsMap[key].(string)
	if !ok {
		return ""
	}
	return val
}
