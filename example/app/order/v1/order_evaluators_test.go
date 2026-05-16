package orderv1_test

import (
	"context"
	"errors"
	"testing"

	"github.com/funinthecloud/protosource"
	orderv1 "github.com/funinthecloud/protosource/example/app/order/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
)

// These tests illustrate the three return paths of a CommandEvaluator:
// nil (proceed), protosource.ErrSkip (silent no-op), and an arbitrary error
// (abort). They double as the reference implementation called out from
// docs/consumer-guide.md.

func newOrderRepo(t *testing.T) *orderv1.Repository {
	t.Helper()
	return orderv1.ProvideRepository(memorystore.New(0), protobinaryserializer.NewSerializer())
}

func seedDraftOrder(t *testing.T, repo *orderv1.Repository) {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.Apply(ctx, &orderv1.Create{
		Id:           "order-1",
		Actor:        "alice",
		CustomerId:   "cust-1",
		CustomerName: "Alice",
	}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if _, err := repo.Apply(ctx, &orderv1.AddItem{
		Id:    "order-1",
		Actor: "alice",
		Item: &orderv1.LineItem{
			ItemId:      "sku-1",
			Description: "Widget",
			PriceCents:  1000,
			Quantity:    1,
		},
	}); err != nil {
		t.Fatalf("seed AddItem: %v", err)
	}
}

// RemoveItem on a present item proceeds (Evaluate returns nil).
func TestRemoveItem_Present_Proceeds(t *testing.T) {
	repo := newOrderRepo(t)
	seedDraftOrder(t, repo)
	ctx := context.Background()

	before, _ := repo.Load(ctx, "order-1")
	beforeVersion := before.(*orderv1.Order).GetVersion()

	v, err := repo.Apply(ctx, &orderv1.RemoveItem{
		Id:     "order-1",
		Actor:  "alice",
		ItemId: "sku-1",
	})
	if err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if v <= beforeVersion {
		t.Fatalf("expected version to advance past %d, got %d", beforeVersion, v)
	}

	after, _ := repo.Load(ctx, "order-1")
	if _, present := after.(*orderv1.Order).GetItems()["sku-1"]; present {
		t.Errorf("expected sku-1 to be removed")
	}
}

// RemoveItem on a missing item is a silent no-op via ErrSkip. No new
// event is persisted, no error returned to the caller — version stays put.
func TestRemoveItem_Missing_SkipsSilently(t *testing.T) {
	repo := newOrderRepo(t)
	seedDraftOrder(t, repo)
	ctx := context.Background()

	before, _ := repo.Load(ctx, "order-1")
	beforeVersion := before.(*orderv1.Order).GetVersion()

	v, err := repo.Apply(ctx, &orderv1.RemoveItem{
		Id:     "order-1",
		Actor:  "alice",
		ItemId: "sku-does-not-exist",
	})
	if err != nil {
		t.Fatalf("expected nil error for ErrSkip path, got: %v", err)
	}
	if v != beforeVersion {
		t.Fatalf("expected version unchanged at %d, got %d", beforeVersion, v)
	}
}

// AddItem with a duplicate item_id aborts with ErrItemAlreadyPresent.
// Demonstrates the "return error" branch — nothing is persisted and the
// caller sees a real failure rather than a silent no-op.
func TestAddItem_Duplicate_Aborts(t *testing.T) {
	repo := newOrderRepo(t)
	seedDraftOrder(t, repo)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &orderv1.AddItem{
		Id:    "order-1",
		Actor: "alice",
		Item: &orderv1.LineItem{
			ItemId:      "sku-1", // already present from seed
			Description: "Different Widget",
			PriceCents:  9999,
			Quantity:    5,
		},
	})
	if !errors.Is(err, orderv1.ErrItemAlreadyPresent) {
		t.Fatalf("expected ErrItemAlreadyPresent, got: %v", err)
	}

	// The original item is untouched.
	after, _ := repo.Load(ctx, "order-1")
	got := after.(*orderv1.Order).GetItems()["sku-1"]
	if got.GetPriceCents() != 1000 || got.GetQuantity() != 1 {
		t.Errorf("expected original item unchanged, got %+v", got)
	}
}

// Compile-time interface assertions: confirm the example commands satisfy
// protosource.CommandEvaluator. These would fail the build if the signature
// drifted from the framework's expectation.
var (
	_ protosource.CommandEvaluator = (*orderv1.RemoveItem)(nil)
	_ protosource.CommandEvaluator = (*orderv1.AddItem)(nil)
)
