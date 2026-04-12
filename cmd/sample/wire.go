//go:build wireinject

package main

import (
	"github.com/funinthecloud/protosource"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"github.com/goforj/wire"
)

func provideStore() *memorystore.MemoryStore {
	return memorystore.New(samplev1.SnapshotEveryNEvents)
}

func InitializeRepository() *samplev1.Repository {
	wire.Build(
		provideStore,
		wire.Bind(new(protosource.Store), new(*memorystore.MemoryStore)),
		protobinaryserializer.ProviderSet,
		samplev1.ProviderSet,
	)
	return nil
}
