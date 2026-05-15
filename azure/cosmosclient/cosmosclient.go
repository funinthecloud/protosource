// Package cosmosclient defines a shared Azure Cosmos DB (NoSQL API) container
// client interface used across the protosource framework. It is the
// Azure-side counterpart of aws/dynamoclient — a thin abstraction that lets
// stores and the opaquedata layer share a mockable surface while the
// production binding is satisfied by *azcosmos.ContainerClient via the
// Wrap helper.
package cosmosclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// ContainerClient is the Cosmos container interface used by the protosource
// framework. It covers all per-item, query, and batch operations needed by
// both the event store and the opaque data store. Implementations target a
// single container; consumers that need both events and aggregates wire two
// clients.
//
// QueryItems drains all pages internally so callers do not have to manage
// continuation tokens. NewQueryItemsPager exposes the underlying pager for
// callers that need early termination (LoadTail).
type ContainerClient interface {
	CreateItem(ctx context.Context, pk azcosmos.PartitionKey, item []byte, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error)
	UpsertItem(ctx context.Context, pk azcosmos.PartitionKey, item []byte, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error)
	ReadItem(ctx context.Context, pk azcosmos.PartitionKey, itemID string, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error)
	DeleteItem(ctx context.Context, pk azcosmos.PartitionKey, itemID string, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error)
	QueryItems(ctx context.Context, query string, pk azcosmos.PartitionKey, o *azcosmos.QueryOptions) ([][]byte, error)
	NewQueryItemsPager(query string, pk azcosmos.PartitionKey, o *azcosmos.QueryOptions) Pager
	// ExecuteCreateBatch atomically creates all items inside a single
	// partition. Returns BatchError when the transaction did not commit
	// (e.g. status 409 when an event document with the same id — the version
	// number — already exists, the Cosmos analog of Dynamo's conditional
	// write failure).
	ExecuteCreateBatch(ctx context.Context, pk azcosmos.PartitionKey, items [][]byte) error
}

// Pager is a minimal pager interface mirroring azcosmos's runtime.Pager. It
// allows store code to terminate iteration once a target item count has been
// collected without forcing the underlying SDK to drain every page.
type Pager interface {
	More() bool
	NextPage(ctx context.Context) ([][]byte, error)
}

// BatchError describes a failed transactional batch. StatusCode is the
// first non-FailedDependency operation result — typically what callers care
// about. Use errors.As to inspect.
type BatchError struct {
	StatusCode int32
	Message    string
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("cosmos batch failed (status %d): %s", e.StatusCode, e.Message)
}

// IsBatchConflict reports whether err represents a duplicate-key conflict
// (HTTP 409) returned from a transactional batch. Stores use this to map
// version collisions to their domain error.
func IsBatchConflict(err error) bool {
	var b *BatchError
	if !errors.As(err, &b) {
		return false
	}
	return b.StatusCode == http.StatusConflict
}

// Wrap adapts an *azcosmos.ContainerClient to the ContainerClient interface.
func Wrap(c *azcosmos.ContainerClient) ContainerClient {
	return &wrapper{c: c}
}

type wrapper struct {
	c *azcosmos.ContainerClient
}

func (w *wrapper) CreateItem(ctx context.Context, pk azcosmos.PartitionKey, item []byte, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return w.c.CreateItem(ctx, pk, item, o)
}

func (w *wrapper) UpsertItem(ctx context.Context, pk azcosmos.PartitionKey, item []byte, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return w.c.UpsertItem(ctx, pk, item, o)
}

func (w *wrapper) ReadItem(ctx context.Context, pk azcosmos.PartitionKey, itemID string, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return w.c.ReadItem(ctx, pk, itemID, o)
}

func (w *wrapper) DeleteItem(ctx context.Context, pk azcosmos.PartitionKey, itemID string, o *azcosmos.ItemOptions) (azcosmos.ItemResponse, error) {
	return w.c.DeleteItem(ctx, pk, itemID, o)
}

func (w *wrapper) QueryItems(ctx context.Context, query string, pk azcosmos.PartitionKey, o *azcosmos.QueryOptions) ([][]byte, error) {
	pager := w.c.NewQueryItemsPager(query, pk, o)
	var out [][]byte
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("cosmosclient: query page: %w", err)
		}
		out = append(out, page.Items...)
	}
	return out, nil
}

func (w *wrapper) NewQueryItemsPager(query string, pk azcosmos.PartitionKey, o *azcosmos.QueryOptions) Pager {
	return &sdkPager{p: w.c.NewQueryItemsPager(query, pk, o)}
}

// MaxBatchOperations is the Cosmos transactional-batch ceiling. Callers must
// pre-batch larger writes themselves; ExecuteCreateBatch refuses to forward a
// request that would exceed this limit rather than letting the SDK fail with
// an opaque server-side error.
const MaxBatchOperations = 100

func (w *wrapper) ExecuteCreateBatch(ctx context.Context, pk azcosmos.PartitionKey, items [][]byte) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) > MaxBatchOperations {
		return fmt.Errorf("cosmosclient: batch of %d items exceeds Cosmos cap of %d", len(items), MaxBatchOperations)
	}
	batch := w.c.NewTransactionalBatch(pk)
	for _, it := range items {
		batch.CreateItem(it, nil)
	}
	resp, err := w.c.ExecuteTransactionalBatch(ctx, batch, nil)
	if err != nil {
		return fmt.Errorf("cosmosclient: execute batch: %w", err)
	}
	if resp.Success {
		return nil
	}
	// The first non-FailedDependency result is the actual cause; the rest are
	// dependency rollbacks reflecting transactional all-or-nothing semantics.
	for _, r := range resp.OperationResults {
		if r.StatusCode != http.StatusFailedDependency {
			return &BatchError{StatusCode: r.StatusCode, Message: string(r.ResourceBody)}
		}
	}
	return &BatchError{StatusCode: 0, Message: "transactional batch failed without a cause status"}
}

// sdkPager adapts the SDK's generic pager to the local Pager interface.
type sdkPager struct {
	p interface {
		More() bool
		NextPage(context.Context) (azcosmos.QueryItemsResponse, error)
	}
}

func (s *sdkPager) More() bool { return s.p.More() }

func (s *sdkPager) NextPage(ctx context.Context) ([][]byte, error) {
	page, err := s.p.NextPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("cosmosclient: query page: %w", err)
	}
	return page.Items, nil
}
