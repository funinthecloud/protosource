# User-assigned Managed Identity attached to the Container App. User-assigned
# (not system-assigned) so:
#
#   1. The identity outlives the app — destroying / recreating the app does
#      not invalidate role assignments granted to it.
#   2. Other modules (e.g. cosmos-eventstore) can reference principal_id
#      *before* the app exists, breaking the chicken-and-egg between RBAC
#      and the workload that consumes it.
#
# The principal_id output feeds straight into cosmos-eventstore's
# data_contributor_principal_ids variable.
resource "azurerm_user_assigned_identity" "app" {
  name                = "${var.name_prefix}-id"
  resource_group_name = var.resource_group_name
  location            = var.location
  tags                = var.tags
}
