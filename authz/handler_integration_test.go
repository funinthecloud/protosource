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
	historyv1 "github.com/funinthecloud/protosource/history/v1"
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

func TestGeneratedHandlerMapsUnknownErrorsToServiceUnavailable(t *testing.T) {
	// Unknown errors from the Authorizer are mapped to 503 so clients,
	// load balancers, and monitoring can distinguish "the authorizer
	// is unreachable" (transient, retry) from "you lack permission"
	// (permanent, do not retry). The request is still rejected — the
	// pipeline does not run — so this is still fail-closed; it just
	// honestly reports WHY it is closed.
	custom := errors.New("auth service connection refused")
	h := newTestHandler(&fakeAuthorizer{returnErr: custom})

	resp := h.HandleCreate(context.Background(), protosource.Request{Actor: "someone"})

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want %d (unknown errors should be 503, not 403)", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if !strings.Contains(resp.Body, "AUTHZ_UNAVAILABLE") {
		t.Errorf("body %q missing AUTHZ_UNAVAILABLE code", resp.Body)
	}
	// Detail must NOT leak: the raw error message from the authorizer
	// might contain internal infrastructure hints.
	if strings.Contains(resp.Body, "connection refused") {
		t.Errorf("body %q leaked internal error detail", resp.Body)
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

// capturingRepo is a minimal Repo implementation that records the last
// applied command without touching any persistence. Used to observe
// what the generated handler writes to cmd.Actor.
type capturingRepo struct {
	lastCmd protosource.Commander
}

func (r *capturingRepo) Apply(_ context.Context, cmd protosource.Commander) (int64, error) {
	r.lastCmd = cmd
	return 1, nil
}

func (r *capturingRepo) Load(_ context.Context, _ string) (protosource.Aggregate, error) {
	return nil, protosource.ErrAggregateNotFound
}

func (r *capturingRepo) History(_ context.Context, _ string) (*historyv1.History, error) {
	return &historyv1.History{}, nil
}

// validCreateBody is the minimum JSON for samplev1.Create to pass
// validation: an id is required.
const validCreateBody = `{"id":"sample-actor-test"}`

func TestGeneratedHandlerPrefersAuthzContextUserIDAsActor(t *testing.T) {
	// The key behavior: when Authorize enriches the context with a
	// user id (via authz.WithUserID), the handler uses THAT as the
	// command's Actor field — not the raw request.Actor. This keeps
	// the audit trail clean in shadow-token flows where request.Actor
	// is the opaque bearer token.
	fake := &fakeAuthorizer{
		enrichCtx: func(ctx context.Context) context.Context {
			return authz.WithUserID(ctx, "user-from-ctx")
		},
	}
	repo := &capturingRepo{}
	h := samplev1.NewHandler(repo, nil, fake)

	resp := h.HandleCreate(context.Background(), protosource.Request{
		Actor:   "raw-shadow-token",
		Body:    validCreateBody,
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, body=%s", resp.StatusCode, resp.Body)
	}
	if repo.lastCmd == nil {
		t.Fatal("repo.lastCmd is nil; Apply was not called")
	}
	cmd, ok := repo.lastCmd.(*samplev1.Create)
	if !ok {
		t.Fatalf("Apply received %T, want *samplev1.Create", repo.lastCmd)
	}
	if cmd.GetActor() != "user-from-ctx" {
		t.Errorf("cmd.Actor = %q, want %q (context user id should win over request.Actor)", cmd.GetActor(), "user-from-ctx")
	}
}

func TestGeneratedHandlerFallsBackToRequestActorWhenContextEmpty(t *testing.T) {
	// When the Authorizer does not enrich the context (e.g.
	// allowall.Authorizer), the handler falls back to the Actor from
	// request.Actor populated by the adapter's ActorExtractor. This
	// preserves the pre-phase-11 developer flow where X-Actor alone
	// is enough.
	fake := &fakeAuthorizer{} // returnErr nil, enrichCtx nil
	repo := &capturingRepo{}
	h := samplev1.NewHandler(repo, nil, fake)

	resp := h.HandleCreate(context.Background(), protosource.Request{
		Actor:   "x-actor-header-value",
		Body:    validCreateBody,
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, body=%s", resp.StatusCode, resp.Body)
	}
	cmd := repo.lastCmd.(*samplev1.Create)
	if cmd.GetActor() != "x-actor-header-value" {
		t.Errorf("cmd.Actor = %q, want %q (request.Actor fallback)", cmd.GetActor(), "x-actor-header-value")
	}
}

func TestGeneratedHandlerRequiresSomeActorEvenWhenAuthzPasses(t *testing.T) {
	// With both the context user id and request.Actor empty, the
	// handler must still 401 with CMD_NO_ACTOR — a successful
	// Authorize call does not imply an identity is present.
	fake := &fakeAuthorizer{}
	repo := &capturingRepo{}
	h := samplev1.NewHandler(repo, nil, fake)

	resp := h.HandleCreate(context.Background(), protosource.Request{
		Actor: "",
		Body:  validCreateBody,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", resp.StatusCode)
	}
	if repo.lastCmd != nil {
		t.Errorf("Apply was called despite no actor: %+v", repo.lastCmd)
	}
}
