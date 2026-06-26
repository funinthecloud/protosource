package orderv1_test

import (
	"context"
	"testing"

	orderv1 "github.com/funinthecloud/protosource/gen/example/app/order/v1"
)

// TestBilling_SetThenClear exercises the singular embedded-message convention
// end to end through Apply -> Load.
//
// It guards the load-bearing semantic the whole convention rests on: On()
// applies a singular embed by NAME via an *unconditional* copy
// (aggregate.Billing = e.GetBilling()). A populated same-named embed therefore
// sets the field, and a clear command — which carries no embed, so the emitted
// event's billing field is nil — clears it. If the generated copy ever became
// nil-guarded, the clear would silently stop working and this test would fail.
func TestBilling_SetThenClear(t *testing.T) {
	repo := newOrderRepo(t)
	ctx := context.Background()

	if _, err := repo.Apply(ctx, &orderv1.Create{
		Id:           "order-b",
		Actor:        "alice",
		CustomerId:   "cust-1",
		CustomerName: "Alice",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Set: the command carries a populated, same-named embed.
	if _, err := repo.Apply(ctx, &orderv1.SetBilling{
		Id:      "order-b",
		Actor:   "alice",
		Billing: &orderv1.Billing{Method: "card", Reference: "ref-123"},
	}); err != nil {
		t.Fatalf("SetBilling: %v", err)
	}

	got, err := repo.Load(ctx, "order-b")
	if err != nil {
		t.Fatalf("Load after set: %v", err)
	}
	billing := got.(*orderv1.Order).GetBilling()
	if billing == nil {
		t.Fatal("after SetBilling, billing is nil; want populated")
	}
	if billing.GetMethod() != "card" || billing.GetReference() != "ref-123" {
		t.Errorf("billing = %+v, want method=card reference=ref-123", billing)
	}

	// Clear: the command carries no embed, so the event's billing field is empty
	// and the unconditional copy nils the aggregate field.
	if _, err := repo.Apply(ctx, &orderv1.ClearBilling{
		Id:    "order-b",
		Actor: "alice",
	}); err != nil {
		t.Fatalf("ClearBilling: %v", err)
	}

	got, err = repo.Load(ctx, "order-b")
	if err != nil {
		t.Fatalf("Load after clear: %v", err)
	}
	if b := got.(*orderv1.Order).GetBilling(); b != nil {
		t.Errorf("after ClearBilling, billing = %+v, want nil", b)
	}
}
