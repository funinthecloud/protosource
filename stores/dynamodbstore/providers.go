package dynamodbstore

import (
	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/aws/dynamoclient"
	opaquedynamo "github.com/funinthecloud/protosource/opaquedata/dynamo"
	"github.com/goforj/wire"
)

// EventsTableName is a named type for Wire to distinguish table name strings.
type EventsTableName string

// AggregatesTableName is a named type for Wire to distinguish table name strings.
type AggregatesTableName string

func ProvideOpaqueStore(client dynamoclient.Client, table AggregatesTableName) *opaquedynamo.Store {
	return opaquedynamo.New(client, string(table))
}

func ProvideStore(client dynamoclient.Client, opaqueStore *opaquedynamo.Store, table EventsTableName) (*DynamoDBStore, error) {
	return New(client,
		WithEventsTable(string(table)),
		WithOpaqueStore(opaqueStore),
	)
}

// ProviderSet provides the DynamoDB event store, opaque store, and binds to
// protosource interfaces. The consumer must provide a dynamoclient.Client and
// the table name types.
var ProviderSet = wire.NewSet(
	ProvideOpaqueStore,
	ProvideStore,
	wire.Bind(new(protosource.Store), new(*DynamoDBStore)),
	wire.Bind(new(protosource.AggregateStore), new(*DynamoDBStore)),
)
