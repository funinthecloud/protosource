//go:build wireinject

package protojsonserializer

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet provides a protojsonserializer and binds it to protosource.Serializer.
var ProviderSet = wire.NewSet(
	NewSerializer,
	wire.Bind(new(protosource.Serializer), new(*Serializer)),
)
