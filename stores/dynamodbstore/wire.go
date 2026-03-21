//go:build wireinject

package dynamodbstore

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet binds *DynamoDBStore to protosource.Store. The consumer must
// provide their own DynamoDBStore constructor (New requires a Dynamoer client).
var ProviderSet = wire.NewSet(
	wire.Bind(new(protosource.Store), new(*DynamoDBStore)),
)
