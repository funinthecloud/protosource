//go:build wireinject

package main

import (
	"github.com/funinthecloud/protosource"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"github.com/google/wire"
)

func provideStore() *memorystore.MemoryStore {
	return memorystore.New(memorystore.WithSnapshotInterval(samplev1.SnapshotEveryNEvents))
}

func provideRepository(store *memorystore.MemoryStore, serializer *protobinaryserializer.Serializer) *protosource.Repository {
	return samplev1.NewRepository(protosource.WithStore(store), protosource.WithSerializer(serializer))
}

func InitializeRepository() *protosource.Repository {
	wire.Build(
		provideStore,
		protobinaryserializer.ProviderSet,
		provideRepository,
	)
	return nil
}
