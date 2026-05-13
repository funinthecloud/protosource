output "principal_id" {
  description = "Object ID of the user-assigned Managed Identity. Feed this into cosmos-eventstore.data_contributor_principal_ids (or any other RBAC grant) to give the running app data-plane access."
  value       = azurerm_user_assigned_identity.app.principal_id
}

output "identity_id" {
  description = "Resource ID of the user-assigned Managed Identity. Use when another module needs to attach the same identity (e.g. additional Container Apps in the same environment)."
  value       = azurerm_user_assigned_identity.app.id
}

output "container_app_fqdn" {
  description = "Public hostname of the Container App (https://<fqdn>)."
  value       = azurerm_container_app.this.latest_revision_fqdn
}

output "container_app_id" {
  description = "Container App resource ID."
  value       = azurerm_container_app.this.id
}

output "container_app_environment_id" {
  description = "Container Apps environment resource ID. Reuse across additional Container Apps to share log analytics and intra-env service discovery."
  value       = azurerm_container_app_environment.this.id
}

output "acr_login_server" {
  description = "ACR login server hostname (<registry>.azurecr.io). Pass to `docker push` / build pipelines."
  value       = azurerm_container_registry.this.login_server
}

output "acr_id" {
  description = "ACR resource ID."
  value       = azurerm_container_registry.this.id
}

output "log_analytics_workspace_id" {
  description = "Workspace ID — either the caller-supplied one or the workspace this module created."
  value       = local.log_analytics_workspace_id
}
