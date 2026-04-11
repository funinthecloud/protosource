package authz_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/authz"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
)

// fakeAuthorizer is a test double for authz.Authorizer that records the
// arguments it was called with and returns a configured error.
type fakeAuthorizer struct {
	// returnErr is the error Authorize returns. If nil, Authorize succeeds.
	returnErr error
	// enrichCtx, when non-nil, is applied to the returned context.
	enrichCtx func(context.Context) context.Context

	// Captured call state:
	calls                int
	lastRequiredFunction string
	lastRequest          protosource.Request
}

func (f *fakeAuthorizer) Authorize(ctx context.Context, req protosource.Request, requiredFunction string) (context.Context, error) {
	f.calls++
	f.lastRequiredFunction = requiredFunction
	f.lastRequest = req
	if f.returnErr != nil {
		return ctx, f.returnErr
	}
	if f.enrichCtx != nil {
		ctx = f.enrichCtx(ctx)
	}
	return ctx, nil
}

// newTestHandler constructs a samplev1.Handler with nil repo/client. This is
// only safe when the test paths do not reach Repository.Apply or client
// methods — i.e., tests that observe the authz layer's short-circuit
// behavior before any pipeline work begins.
func newTestHandler(a authz.Authorizer) *samplev1.Handler {
	return samplev1.NewHandler(nil, nil, a)
}

func TestGeneratedHandlerCallsAuthorizeWithCanonicalFunctionName(t *testing.T) {
	fake := &fakeAuthorizer{returnErr: authz.ErrForbidden}
	h := newTestHandler(fake)

	_ = h.HandleCreate(context.Background(), protosource.Request{Actor: "someone"})

	if fake.calls != 1 {
		t.Fatalf("authorizer called %d times, want 1", fake.calls)
	}
	const want = "example.app.sample.v1.Create"
	if fake.lastRequiredFunction != want {
		t.Errorf("requiredFunction = %q, want %q", fake.lastRequiredFunction, want)
	}
}

func TestGeneratedHandlerMapsUnauthenticatedTo401(t *testing.T) {
	h := newTestHandler(&fakeAuthorizer{returnErr: authz.ErrUnauthenticated})

	resp := h.HandleCreate(context.Background(), protosource.Request{Actor: "someone"})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestGeneratedHandlerMapsForbiddenTo403(t *testing.T) {
	h := newTestHandler(&fakeAuthorizer{returnErr: authz.ErrForbidden})

	resp := h.HandleCreate(context.Background(), protosource.Request{Actor: "someone"})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestGeneratedHandlerMapsUnknownErrorsToForbidden(t *testing.T) {
	// Conservative default: unknown errors from the Authorizer are treated
	// as forbidden, not 500 — failing closed is safer than failing open.
	custom := errors.New("custom policy engine exploded")
	h := newTestHandler(&fakeAuthorizer{returnErr: custom})

	resp := h.HandleCreate(context.Background(), protosource.Request{Actor: "someone"})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestGeneratedHandlerShortCircuitsBeforePipeline(t *testing.T) {
	// If authz fails, the handler must return without touching repo or
	// attempting to unmarshal. We prove this indirectly by passing nil
	// repo/client/body — if the handler tried to touch any of them it
	// would panic.
	h := newTestHandler(&fakeAuthorizer{returnErr: authz.ErrForbidden})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handler panicked after authz failure: %v", r)
		}
	}()

	// No Actor, no Body, nil repo/client — only safe because authz fails first.
	_ = h.HandleCreate(context.Background(), protosource.Request{})
}

func TestGeneratedNewHandlerPanicsOnNilAuthorizer(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewHandler(nil authorizer) did not panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T %v, want string", r, r)
		}
		// Must name the package, the constructor, and point to the fix.
		for _, want := range []string{"samplev1.NewHandler", "authorizer", "allowall"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic message %q missing %q", msg, want)
			}
		}
	}()
	_ = samplev1.NewHandler(nil, nil, nil)
}

func TestGeneratedHandlerPassesAuthzThenChecksActor(t *testing.T) {
	// With a passing authz and empty Actor, the handler proceeds past
	// Authorize and hits the CMD_NO_ACTOR check, returning 401. This
	// confirms the authz call is non-blocking when it returns nil.
	fake := &fakeAuthorizer{returnErr: nil}
	h := newTestHandler(fake)

	resp := h.HandleCreate(context.Background(), protosource.Request{Actor: ""})

	if fake.calls != 1 {
		t.Errorf("authorizer called %d times, want 1", fake.calls)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d (CMD_NO_ACTOR after authz pass-through)", resp.StatusCode, http.StatusUnauthorized)
	}
}
