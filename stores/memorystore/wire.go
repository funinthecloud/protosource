//go:build wireinject

package memorystore

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet provides a default MemoryStore and binds it to protosource.Store.
// For snapshot-aware configuration, pass options via your own provider and use
// just the Bind from this set, or call New directly with WithSnapshotInterval.
var ProviderSet = wire.NewSet(
	New,
	wire.Bind(new(protosource.Store), new(*MemoryStore)),
)
