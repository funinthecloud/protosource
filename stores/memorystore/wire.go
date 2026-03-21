//go:build wireinject

package memorystore

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet binds *MemoryStore to protosource.Store. The consumer must
// provide their own MemoryStore constructor (New requires snapshotInterval).
var ProviderSet = wire.NewSet(
	wire.Bind(new(protosource.Store), new(*MemoryStore)),
)
