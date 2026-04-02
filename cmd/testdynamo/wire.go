//go:build wireinject

package main

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/wire"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/aws/dynamoclient"
	orderv1 "github.com/funinthecloud/protosource/example/app/order/v1"
	orderv1dynamodb "github.com/funinthecloud/protosource/example/app/order/v1/orderv1dynamodb"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	samplev1dynamodb "github.com/funinthecloud/protosource/example/app/sample/v1/samplev1dynamodb"
	testv1 "github.com/funinthecloud/protosource/example/app/test/v1"
	testv1dynamodb "github.com/funinthecloud/protosource/example/app/test/v1/testv1dynamodb"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedynamo "github.com/funinthecloud/protosource/opaquedata/dynamo"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/dynamodbstore"
)

func provideRouter(
	testHandler *testv1.Handler,
	orderHandler *orderv1.Handler,
	sampleHandler *samplev1.Handler,
) *protosource.Router {
	return protosource.NewRouter(testHandler, orderHandler, sampleHandler)
}

// InitializeRouter wires all dependencies and returns a configured router.
func InitializeRouter(
	client *dynamodb.Client,
	eventsTable dynamodbstore.EventsTableName,
	aggregatesTable dynamodbstore.AggregatesTableName,
) (*protosource.Router, error) {
	wire.Build(
		wire.Bind(new(dynamoclient.Client), new(*dynamodb.Client)),
		wire.Bind(new(opaquedata.OpaqueStore), new(*opaquedynamo.Store)),
		dynamodbstore.ProviderSet,
		protobinaryserializer.ProviderSet,
		testv1dynamodb.ProviderSet,
		orderv1dynamodb.ProviderSet,
		samplev1dynamodb.ProviderSet,
		testv1.NewTestClient,
		orderv1.NewOrderClient,
		samplev1.NewSampleClient,
		testv1.NewHandler,
		orderv1.NewHandler,
		samplev1.NewHandler,
		provideRouter,
	)
	return nil, nil
}
