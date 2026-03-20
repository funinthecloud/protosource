package protosource_test

import (
	"context"
	"strings"
	"testing"

	"github.com/funinthecloud/protosource"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"google.golang.org/protobuf/proto"
)

func newCompressedRepo(threshold int, storeOpts ...memorystore.Option) (*protosource.Repository, *memorystore.MemoryStore) {
	store := memorystore.New(storeOpts...)
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
		protosource.WithCompression(threshold),
	)
	return repo, store
}

func TestCompression_EventRoundTrip(t *testing.T) {
	// Use a low threshold so small test data gets compressed
	repo, _ := newCompressedRepo(10)
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
	if test.GetVersion() != 3 {
		t.Errorf("expected version 3, got %d", test.GetVersion())
	}
	if test.GetState() != testv1.State_STATE_UNLOCKED {
		t.Errorf("expected STATE_UNLOCKED, got %s", test.GetState())
	}
}

func TestCompression_AggregateStoreRoundTrip(t *testing.T) {
	repo, store := newCompressedRepo(10)
	ctx := context.Background()

	// Use a large body to ensure it exceeds the threshold
	largeBody := strings.Repeat("x", 500)
	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: largeBody})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify the stored aggregate data is compressed (gzip magic bytes)
	data, version, err := store.LoadAggregate(ctx, "id-1")
	if err != nil {
		t.Fatalf("load aggregate failed: %v", err)
	}
	if version != 2 {
		t.Errorf("expected version 2, got %d", version)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Error("expected aggregate data to be gzip compressed")
	}

	// Verify it's smaller than the uncompressed proto
	agg := &testv1.Test{Id: "id-1", Body: largeBody, Version: 2}
	uncompressed, _ := proto.Marshal(agg)
	if len(data) >= len(uncompressed) {
		t.Errorf("expected compressed data (%d bytes) to be smaller than uncompressed (%d bytes)", len(data), len(uncompressed))
	}
}

func TestCompression_BelowThresholdNotCompressed(t *testing.T) {
	// Set threshold high enough that test data won't hit it
	repo, store := newCompressedRepo(100_000)
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "tiny"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Stored aggregate data should NOT be gzip (below threshold)
	data, _, err := store.LoadAggregate(ctx, "id-1")
	if err != nil {
		t.Fatalf("load aggregate failed: %v", err)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		t.Error("expected aggregate data to NOT be compressed (below threshold)")
	}

	// Should still load correctly
	agg, err := repo.Load(ctx, "id-1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if agg.(*testv1.Test).GetBody() != "tiny" {
		t.Errorf("expected body 'tiny', got %q", agg.(*testv1.Test).GetBody())
	}
}

func TestCompression_DisabledByDefault(t *testing.T) {
	// No WithCompression — threshold is 0 (disabled)
	store := memorystore.New()
	repo := protosource.New(
		&testv1.Test{},
		protosource.WithStore(store),
		protosource.WithSerializer(protobinaryserializer.NewSerializer()),
	)
	ctx := context.Background()

	largeBody := strings.Repeat("x", 500)
	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: largeBody})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Stored data should NOT be compressed
	data, _, _ := store.LoadAggregate(ctx, "id-1")
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		t.Error("expected aggregate data to NOT be compressed when compression is disabled")
	}
}

func TestCompression_BackwardCompat_UncompressedDataLoads(t *testing.T) {
	// Write without compression, then read with compression enabled.
	// This simulates loading old uncompressed data after enabling compression.
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
	repo, _ := newCompressedRepo(10, memorystore.WithSnapshotInterval(3))
	ctx := context.Background()

	_, err := repo.Apply(ctx, &testv1.Create{Id: "id-1", Actor: "actor", Body: "snapshot test body"})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// This update triggers a snapshot at v3
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

func TestCompression_MultipleAggregatesIndependent(t *testing.T) {
	repo, _ := newCompressedRepo(10)
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
