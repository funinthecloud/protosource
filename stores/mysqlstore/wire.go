//go:build wireinject

package mysqlstore

import (
	"github.com/funinthecloud/protosource"
	"github.com/google/wire"
)

// ProviderSet binds *MySQLStore to protosource.Store. The consumer must
// provide their own MySQLStore constructor (NewMySQLStore requires a *sql.DB).
var ProviderSet = wire.NewSet(
	wire.Bind(new(protosource.Store), new(*MySQLStore)),
)
