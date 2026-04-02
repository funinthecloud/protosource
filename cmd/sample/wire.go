//go:build wireinject

package main

import (
	"github.com/funinthecloud/protosource"
	samplev1 "github.com/funinthecloud/protosource/example/app/sample/v1"
	samplev1memory "github.com/funinthecloud/protosource/example/app/sample/v1/samplev1memory"
	"github.com/funinthecloud/protosource/serializers/protobinaryserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"
	"github.com/google/wire"
)

func provideStore() *memorystore.MemoryStore {
	return memorystore.New(samplev1.SnapshotEveryNEvents)
}

func InitializeRepository() *samplev1memory.Repository {
	wire.Build(
		provideStore,
		wire.Bind(new(protosource.Store), new(*memorystore.MemoryStore)),
		protobinaryserializer.ProviderSet,
		samplev1memory.ProviderSet,
	)
	return nil
}
