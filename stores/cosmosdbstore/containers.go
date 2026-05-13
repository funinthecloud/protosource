package cosmosdbstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// NumGSIs is the number of GSI slot pairs projected onto the aggregates
// container. The opaquedata model preallocates 20 slots; with PAY-PER-REQUEST
// / serverless billing, empty slots cost nothing.
const NumGSIs = 20

// defaultTTLMinusOne enables per-item `ttl` overrides on a container without
// expiring untagged items. Cosmos interprets -1 on the container as
// "auto-purge enabled, no default — only items with their own `ttl` value
// expire". This matches the Dynamo TTL-on-attribute model.
var defaultTTLMinusOne = int32(-1)

// EnsureDatabase idempotently creates the Cosmos database. If it already
// exists, no action is taken. A 409 Conflict from CreateDatabase is treated
// as success so concurrent startup/deploys can race safely.
func EnsureDatabase(ctx context.Context, client *azcosmos.Client, databaseID string) (*azcosmos.DatabaseClient, error) {
	db, err := client.NewDatabase(databaseID)
	if err != nil {
		return nil, fmt.Errorf("cosmosdbstore.EnsureDatabase: handle: %w", err)
	}
	if _, err := db.Read(ctx, nil); err == nil {
		return db, nil
	} else if !isNotFound(err) {
		return nil, fmt.Errorf("cosmosdbstore.EnsureDatabase: read: %w", err)
	}
	if _, err := client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: databaseID}, nil); err != nil {
		if !isConflict(err) {
			return nil, fmt.Errorf("cosmosdbstore.EnsureDatabase: create: %w", err)
		}
	}
	return db, nil
}

// EnsureContainers idempotently creates the events and aggregates containers
// inside the supplied database. Both containers are created with
// DefaultTimeToLive = -1 so per-item `ttl` overrides expire records, mirroring
// the DynamoDB TTL-on-attribute behavior.
//
// Partition keys:
//   - events:     /a   (aggregate ID)
//   - aggregates: /pk  (opaquedata partition key)
//
// Indexing falls back to Cosmos defaults (index all paths), which is enough
// for the predicate + single-property ORDER BY queries the framework issues.
// Composite indexes on (gsiNpk, gsiNsk) can be layered on later if a workload
// proves it needs them.
func EnsureContainers(ctx context.Context, db *azcosmos.DatabaseClient, eventsContainer, aggregatesContainer string) error {
	if err := ensureContainer(ctx, db, eventsContainer, "/"+attrPartitionKey); err != nil {
		return fmt.Errorf("ensure events container %q: %w", eventsContainer, err)
	}
	if err := ensureContainer(ctx, db, aggregatesContainer, "/pk"); err != nil {
		return fmt.Errorf("ensure aggregates container %q: %w", aggregatesContainer, err)
	}
	return nil
}

func ensureContainer(ctx context.Context, db *azcosmos.DatabaseClient, id, partitionKeyPath string) error {
	c, err := db.NewContainer(id)
	if err != nil {
		return fmt.Errorf("handle: %w", err)
	}
	if _, err := c.Read(ctx, nil); err == nil {
		return nil
	} else if !isNotFound(err) {
		return fmt.Errorf("read: %w", err)
	}
	props := azcosmos.ContainerProperties{
		ID: id,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
			Paths: []string{partitionKeyPath},
		},
		DefaultTimeToLive: &defaultTTLMinusOne,
	}
	if _, err := db.CreateContainer(ctx, props, nil); err != nil {
		// A 409 here means another process won the race between our Read
		// and CreateContainer. The container exists, which is what the
		// caller asked for — treat as success.
		if !isConflict(err) {
			return fmt.Errorf("create: %w", err)
		}
	}
	return nil
}

func isNotFound(err error) bool {
	var rerr *azcore.ResponseError
	if errors.As(err, &rerr) {
		return rerr.StatusCode == http.StatusNotFound
	}
	return false
}

func isConflict(err error) bool {
	var rerr *azcore.ResponseError
	if errors.As(err, &rerr) {
		return rerr.StatusCode == http.StatusConflict
	}
	return false
}
