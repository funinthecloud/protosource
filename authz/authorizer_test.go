package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/authz"
	"github.com/funinthecloud/protosource/authz/allowall"
)

func TestAllowAllPermitsEveryRequest(t *testing.T) {
	var a authz.Authorizer = allowall.Authorizer{}

	ctx := context.Background()
	req := protosource.Request{Actor: "anyone"}

	gotCtx, err := a.Authorize(ctx, req, "example.v1.AnyCommand")
	if err != nil {
		t.Fatalf("allowall.Authorize returned error: %v", err)
	}
	if gotCtx != ctx {
		t.Errorf("allowall.Authorize returned a different context; expected pass-through")
	}
}

func TestUserIDContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := authz.UserIDFromContext(ctx); got != "" {
		t.Errorf("UserIDFromContext on empty context = %q, want empty", got)
	}

	ctx = authz.WithUserID(ctx, "user-42")
	if got := authz.UserIDFromContext(ctx); got != "user-42" {
		t.Errorf("UserIDFromContext = %q, want %q", got, "user-42")
	}
}

func TestJWTContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := authz.JWTFromContext(ctx); got != "" {
		t.Errorf("JWTFromContext on empty context = %q, want empty", got)
	}

	ctx = authz.WithJWT(ctx, "eyJraWQiOi4uLg")
	if got := authz.JWTFromContext(ctx); got != "eyJraWQiOi4uLg" {
		t.Errorf("JWTFromContext = %q, want %q", got, "eyJraWQiOi4uLg")
	}
}

func TestTypedErrorsAreDistinct(t *testing.T) {
	if errors.Is(authz.ErrUnauthenticated, authz.ErrForbidden) {
		t.Errorf("ErrUnauthenticated and ErrForbidden must not be equivalent")
	}
	if errors.Is(authz.ErrForbidden, authz.ErrUnauthenticated) {
		t.Errorf("ErrForbidden and ErrUnauthenticated must not be equivalent")
	}
}
