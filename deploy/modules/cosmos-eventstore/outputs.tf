output "account_name" {
  description = "Cosmos account name."
  value       = azurerm_cosmosdb_account.this.name
}

output "endpoint" {
  description = "Cosmos account endpoint URL (https://<account>.documents.azure.com:443/). Pass to azcosmos.NewClient / NewClientWithKey."
  value       = azurerm_cosmosdb_account.this.endpoint
}

output "primary_key" {
  description = "Account primary master key. Prefer Managed Identity + the data_contributor_principal_ids variable over distributing this key."
  value       = azurerm_cosmosdb_account.this.primary_key
  sensitive   = true
}

output "database_name" {
  description = "SQL database id."
  value       = azurerm_cosmosdb_sql_database.this.name
}

output "events_container_name" {
  description = "Events container id."
  value       = azurerm_cosmosdb_sql_container.events.name
}

output "aggregates_container_name" {
  description = "Aggregates container id."
  value       = azurerm_cosmosdb_sql_container.aggregates.name
}

output "account_id" {
  description = "Fully qualified Cosmos account resource ID. Useful for downstream modules that need to scope policies or diagnostic settings."
  value       = azurerm_cosmosdb_account.this.id
}
