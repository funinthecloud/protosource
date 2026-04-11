// Package authz defines the Authorizer contract that every generated command
// handler invokes before its pipeline runs.
//
// The protosource plugin stamps a canonical function name ({proto_package}.
// {CommandMessageName}, e.g. "example.app.sample.v1.Create") into each
// generated handler at code-generation time. At request time the handler
// calls Authorizer.Authorize with that name, letting the implementation
// decide whether the caller is allowed to proceed.
//
// The framework ships [allowall.Authorizer] as the default binding so that
// generated code compiles and runs out of the box with no real authorization
// wired up. Production deployments override that binding with a concrete
// implementation — for example, the shadow-token authorizer published by the
// protosource-auth project.
package authz

import (
	"context"
	"errors"

	"github.com/funinthecloud/protosource"
)

// Authorizer gates every generated command handler.
//
// Implementations inspect the incoming request (cookies, authorization
// header, etc.) to determine the caller's identity, verify that the caller
// holds requiredFunction, and optionally enrich the returned context with
// identity facts ([WithUserID], [WithJWT]) that downstream handler code can
// read.
//
// The requiredFunction argument is the canonical function name for the
// command being invoked. By convention it is "{proto_package}.{MessageName}"
// (e.g. "example.app.sample.v1.Create"). The protosource plugin generates
// this string at compile time — callers never construct it.
//
// Error semantics mapped by generated handlers:
//
//   - Returning [ErrUnauthenticated] yields HTTP 401.
//   - Returning [ErrForbidden] yields HTTP 403.
//   - Any other non-nil error is treated as [ErrForbidden] for conservative
//     safety — implementations should wrap their internal errors in one of
//     the typed sentinels above when they want a specific status code.
//
// Implementations should be safe for concurrent use.
type Authorizer interface {
	Authorize(ctx context.Context, request protosource.Request, requiredFunction string) (context.Context, error)
}

// ErrUnauthenticated indicates that the caller could not be identified —
// missing, expired, or malformed credentials. Generated handlers map this
// to HTTP 401.
var ErrUnauthenticated = errors.New("authz: unauthenticated")

// ErrForbidden indicates that the caller was identified but does not hold
// the required function. Generated handlers map this to HTTP 403.
var ErrForbidden = errors.New("authz: forbidden")

// ── Context helpers ──
//
// Authorizer implementations use these helpers to stash resolved identity
// facts into the context they return, and downstream application code reads
// them back. The framework itself never inspects these values — they are a
// convenience for handler authors.

type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
	ctxKeyJWT
)

// WithUserID returns a child context carrying the authenticated user id.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

// UserIDFromContext returns the user id stashed by an Authorizer via
// [WithUserID], or "" if none is present.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyUserID).(string); ok {
		return v
	}
	return ""
}

// WithJWT returns a child context carrying a forwarded JWT. Shadow-token
// authorizers that dereference opaque tokens to real JWTs use this so
// downstream handlers can reuse the JWT for outbound service calls.
func WithJWT(ctx context.Context, jwt string) context.Context {
	return context.WithValue(ctx, ctxKeyJWT, jwt)
}

// JWTFromContext returns the JWT stashed by an Authorizer via [WithJWT], or
// "" if none is present.
func JWTFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyJWT).(string); ok {
		return v
	}
	return ""
}
