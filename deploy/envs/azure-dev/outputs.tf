output "resource_group_name" {
  value = azurerm_resource_group.this.name
}

output "container_app_url" {
  description = "Public URL of the Container App. Append /apply, /load, /history routes for the example handlers."
  value       = "https://${module.app.container_app_fqdn}"
}

output "acr_login_server" {
  description = "Push images here. Example: `az acr login --name <login_server>` then `docker push <login_server>/testcosmos:latest`."
  value       = module.app.acr_login_server
}

output "cosmos_account_name" {
  description = "Cosmos account name. Pass to `az cosmosdb` commands."
  value       = module.cosmos.account_name
}

output "cosmos_endpoint" {
  description = "Cosmos account endpoint. Already wired into the app via COSMOS_ENDPOINT — exposed for ad-hoc local clients."
  value       = module.cosmos.endpoint
}

output "cosmos_database_name" {
  description = "Cosmos SQL database id."
  value       = module.cosmos.database_name
}

output "events_container_name" {
  description = "Events container id."
  value       = module.cosmos.events_container_name
}

output "aggregates_container_name" {
  description = "Aggregates container id."
  value       = module.cosmos.aggregates_container_name
}

output "container_app_name" {
  description = "Container App resource name. Pass to `az containerapp logs show`, `az containerapp revision list`, etc."
  value       = "${var.name_prefix}-app"
}

output "managed_identity_principal_id" {
  description = "Object ID of the Container App's Managed Identity — already granted Cosmos Data Contributor. Reuse if you add more data-plane assignments."
  value       = module.app.principal_id
}
