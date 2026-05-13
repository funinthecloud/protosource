# Two containers, mirroring the Go side's cosmosdbstore design:
#
#   events     — partition key /a (aggregate ID), id = strconv(version).
#                Per-partition id uniqueness enforces the version-collision
#                guarantee (the Cosmos analog of Dynamo's conditional write).
#
#   aggregates — partition key /pk (opaquedata pk), id = sk. Holds materialized
#                aggregates with 20 GSI slot pairs that serve as cross-partition
#                query indexes (no per-index objects required in Cosmos).
#
# Both containers set default_ttl = -1, which enables per-item TTL without
# expiring untagged items — the Cosmos analog of Dynamo's TTL-on-attribute.
# The cosmosdbstore writes a per-item `ttl` value (relative seconds) only
# when callers configure TTL.
resource "azurerm_cosmosdb_sql_container" "events" {
  name                  = var.events_container_name
  resource_group_name   = var.resource_group_name
  account_name          = azurerm_cosmosdb_account.this.name
  database_name         = azurerm_cosmosdb_sql_database.this.name
  partition_key_paths   = ["/a"]
  partition_key_version = 2
  default_ttl           = -1

  # Indexing falls back to Cosmos defaults (index every path). Composite
  # indexes on (a, v) would optimize event range scans but are unnecessary
  # for current access patterns — Load and LoadTail stay single-partition.

  # No container-level throughput: serverless accounts forbid it, and when
  # provisioned we set throughput at the database level (see main.tf).
}

resource "azurerm_cosmosdb_sql_container" "aggregates" {
  name                  = var.aggregates_container_name
  resource_group_name   = var.resource_group_name
  account_name          = azurerm_cosmosdb_account.this.name
  database_name         = azurerm_cosmosdb_sql_database.this.name
  partition_key_paths   = ["/pk"]
  partition_key_version = 2
  default_ttl           = -1

  # GSI access patterns are cross-partition queries against gsiNpk/gsiNsk
  # properties. Composite indexes on each (gsiNpk, gsiNsk) pair would speed
  # up sorted GSI scans; they're omitted here to keep the dev module small.
  # Layer them in via a separate optimization PR once a workload demands it.
}
