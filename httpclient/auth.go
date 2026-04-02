package httpclient

import "net/http"

// BearerTokenAuth adds a Bearer token to each request.
type BearerTokenAuth struct {
	token string
	actor string
}

// NewBearerTokenAuth creates an AuthProvider that sets Authorization: Bearer.
func NewBearerTokenAuth(token, actor string) *BearerTokenAuth {
	return &BearerTokenAuth{token: token, actor: actor}
}

func (a *BearerTokenAuth) Authenticate(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+a.token)
	return nil
}

func (a *BearerTokenAuth) Actor() string { return a.actor }

// NoAuth provides actor identity without authentication headers.
// Suitable for local development and testing.
type NoAuth struct {
	actor string
}

// NewNoAuth creates an AuthProvider with no authentication.
func NewNoAuth(actor string) *NoAuth {
	return &NoAuth{actor: actor}
}

func (a *NoAuth) Authenticate(_ *http.Request) error { return nil }
func (a *NoAuth) Actor() string                      { return a.actor }
