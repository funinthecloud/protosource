//go:build wireinject

package boltdbstore

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet binds *BoltDBStore to protosource.Store. The consumer must
// provide their own BoltDBStore constructor (New requires basePath and pkg).
var ProviderSet = wire.NewSet(
	wire.Bind(new(protosource.Store), new(*BoltDBStore)),
)
