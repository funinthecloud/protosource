// Package allowall provides a no-op [authz.Authorizer] that permits every
// request.
//
// It is the framework default binding for [authz.Authorizer] and is
// appropriate for local development, unit tests, and services that enforce
// authorization at a different layer (for example, an upstream gateway).
//
// Never include this in a production wire set unless you are certain that
// authorization is handled elsewhere.
package allowall

import (
	"context"

	"github.com/goforj/wire"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/authz"
)

// Authorizer is a no-op implementation of [authz.Authorizer] that permits
// every request and returns the incoming context unchanged.
type Authorizer struct{}

// Authorize always returns the incoming context unchanged and a nil error.
func (Authorizer) Authorize(ctx context.Context, _ protosource.Request, _ string) (context.Context, error) {
	return ctx, nil
}

// Compile-time assertion that Authorizer satisfies [authz.Authorizer].
var _ authz.Authorizer = Authorizer{}

// Provide returns a no-op Authorizer bound to the [authz.Authorizer]
// interface. This is the wire provider used by [ProviderSet].
func Provide() authz.Authorizer {
	return Authorizer{}
}

// ProviderSet wires the no-op allow-all Authorizer as [authz.Authorizer].
// Include this set in a wire.Build call to get default allow-all semantics.
// Production deployments should provide their own Authorizer binding
// instead of including this set.
var ProviderSet = wire.NewSet(Provide)
