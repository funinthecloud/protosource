# Log Analytics workspace for Container Apps logs/metrics. When the caller
# passes log_analytics_workspace_id, reuse that and skip creation — sharing
# one workspace across multiple modules consolidates queries and reduces
# the per-workspace ingestion overhead.
resource "azurerm_log_analytics_workspace" "this" {
  count = var.log_analytics_workspace_id == null ? 1 : 0

  name                = "${var.name_prefix}-logs"
  resource_group_name = var.resource_group_name
  location            = var.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = var.tags
}

locals {
  log_analytics_workspace_id = (
    var.log_analytics_workspace_id != null
    ? var.log_analytics_workspace_id
    : azurerm_log_analytics_workspace.this[0].id
  )
}

# Container Apps environment: the shared compute + networking + logging
# boundary that one or more apps run inside. Apps in the same environment
# get free intra-environment service discovery.
resource "azurerm_container_app_environment" "this" {
  name                       = "${var.name_prefix}-env"
  resource_group_name        = var.resource_group_name
  location                   = var.location
  log_analytics_workspace_id = local.log_analytics_workspace_id
  tags                       = var.tags
}
