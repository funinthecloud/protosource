//go:build wireinject

package main

import (
	"github.com/goforj/wire"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/authz/allowall"
	orderv1 "github.com/funinthecloud/protosource/gen/example/app/order/v1"
	samplev1 "github.com/funinthecloud/protosource/gen/example/app/sample/v1"
	testv1 "github.com/funinthecloud/protosource/gen/example/app/test/v1"
	"github.com/funinthecloud/protosource/gen/opaquedata"
	opaquecosmos "github.com/funinthecloud/protosource/gen/opaquedata/cosmos"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/cosmosdbstore"
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
	events cosmosdbstore.EventsContainerClient,
	aggregates cosmosdbstore.AggregatesContainerClient,
) (*protosource.Router, error) {
	wire.Build(
		wire.Bind(new(opaquedata.OpaqueStore), new(*opaquecosmos.Store)),
		cosmosdbstore.ProviderSet,
		protobinaryserializer.ProviderSet,
		allowall.ProviderSet,
		testv1.ProviderSet,
		orderv1.ProviderSet,
		samplev1.ProviderSet,
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
