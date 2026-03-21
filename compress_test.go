package protosource_test

import (
	"context"
	"testing"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
)

func newCompressedRepo(threshold int, storeOpts ...memorystore.Option) *protosource.Repository {
	store := memorystore.New(storeOpts...)
	return protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
		protosource.WithCompression(threshold),
	)
}

func TestCompression_EventRoundTrip(t *testing.T) {
	repo := newCompressedRepo(10)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "hello world"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "updated body content"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetBody() != "updated body content" {
		t.Errorf("expected body 'updated body content', got %q", test.GetBody())
	}
	if test.GetVersion() != 4 {
		t.Errorf("expected version 4, got %d", test.GetVersion())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
}

func TestCompression_BackwardCompat_UncompressedDataLoads(t *testing.T) {
	store := memorystore.New()
	ctx := context.Background()

	// Write without compression
	repoNoCompress := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	_, err := repoNoCompress.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "old data"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Read with compression enabled — should still work
	repoWithCompress := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
		protosource.WithCompression(10),
	)
	agg, err := repoWithCompress.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load with compression enabled failed: %v", err)
	}
	if agg.(*testv1.Test).GetBody() != "old data" {
		t.Errorf("expected body 'old data', got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestCompression_WithSnapshots(t *testing.T) {
	repo := newCompressedRepo(10, memorystore.WithSnapshotInterval(3))
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "snapshot test body"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, err = repo.Apply(ctx, &testv1.Update{Id: "id-1", Actor: "actor", Body: "after snapshot"})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load after snapshot failed: %v", err)
	}

	test := agg.(*testv1.Test)
	if test.GetBody() != "after snapshot" {
		t.Errorf("expected body 'after snapshot', got %q", test.GetBody())
	}
}

func TestCompression_ZeroThresholdDisables(t *testing.T) {
	store := memorystore.New()
	ctx := context.Background()

	// WithCompression(0) should disable compression — data stored uncompressed
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
		protosource.WithCompression(0),
	)
	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "zero threshold"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if agg.(*testv1.Test).GetBody() != "zero threshold" {
		t.Errorf("expected body 'zero threshold', got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestCompression_MultipleAggregatesIndependent(t *testing.T) {
	repo := newCompressedRepo(10)
	ctx := context.Background()

	_, _ = repo.Apply(ctx, &testv1.Create{Id: "agg-1", Actor: "actor", Body: "first aggregate"})
	_, _ = repo.Apply(ctx, &testv1.Create{Id: "agg-2", Actor: "actor", Body: "second aggregate"})

	agg1, err := repo.Load(ctx, "agg-1")
	if err != nil {
		t.Fatalf("load agg-1 failed: %v", err)
	}
	agg2, err := repo.Load(ctx, "agg-2")
	if err != nil {
		t.Fatalf("load agg-2 failed: %v", err)
	}

	if agg1.(*testv1.Test).GetBody() != "first aggregate" {
		t.Errorf("agg-1 body mismatch")
	}
	if agg2.(*testv1.Test).GetBody() != "second aggregate" {
		t.Errorf("agg-2 body mismatch")
	}
}
