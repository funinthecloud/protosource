# The Cosmos account itself. Serverless is the default capability and the
# right fit for dev / low-traffic deployments; flip var.serverless to false
# and provision RU/s at the database level for production workloads.
#
# Backup policy: Cosmos serverless only supports Periodic backups. We pin
# Periodic explicitly so the apply doesn't silently downgrade if the
# provider's default ever changes.
resource "azurerm_cosmosdb_account" "this" {
  name                = var.account_name
  resource_group_name = var.resource_group_name
  location            = var.location
  offer_type          = "Standard"
  kind                = "GlobalDocumentDB"

  public_network_access_enabled = var.public_network_access_enabled

  consistency_policy {
    consistency_level = var.consistency_level
  }

  geo_location {
    location          = var.location
    failover_priority = 0
  }

  dynamic "capabilities" {
    for_each = var.serverless ? [1] : []
    content {
      name = "EnableServerless"
    }
  }

  backup {
    type                = "Periodic"
    interval_in_minutes = 240 # 4h
    retention_in_hours  = 168 # 7d
    storage_redundancy  = "Local"
  }

  tags = var.tags
}

resource "azurerm_cosmosdb_sql_database" "this" {
  name                = var.database_name
  resource_group_name = var.resource_group_name
  account_name        = azurerm_cosmosdb_account.this.name

  # Provisioned throughput at the database level applies only when the
  # account is NOT serverless. Cosmos rejects throughput on serverless DBs.
  throughput = var.serverless ? null : var.provisioned_throughput
}
