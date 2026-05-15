package cosmosdbstore

import (
	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	opaquecosmos "github.com/funinthecloud/protosource/opaquedata/cosmos"
	"github.com/goforj/wire"
)

// EventsContainerClient is a Wire-typed alias for the cosmosclient targeting
// the events container. Consumers wire two named clients — one for events,
// one for aggregates — so the wire graph stays unambiguous.
type EventsContainerClient cosmosclient.ContainerClient

// AggregatesContainerClient is a Wire-typed alias for the cosmosclient
// targeting the aggregates container.
type AggregatesContainerClient cosmosclient.ContainerClient

func ProvideOpaqueStore(client AggregatesContainerClient) *opaquecosmos.Store {
	return opaquecosmos.New(client)
}

func ProvideStore(client EventsContainerClient, opaqueStore *opaquecosmos.Store) (*CosmosDBStore, error) {
	return New(client, WithOpaqueStore(opaqueStore))
}

// ProviderSet provides the Cosmos event store, opaque store, and binds to
// the protosource interfaces. The consumer must supply both container clients
// and the typed aliases above.
var ProviderSet = wire.NewSet(
	ProvideOpaqueStore,
	ProvideStore,
	wire.Bind(new(protosource.Store), new(*CosmosDBStore)),
	wire.Bind(new(protosource.AggregateStore), new(*CosmosDBStore)),
)
