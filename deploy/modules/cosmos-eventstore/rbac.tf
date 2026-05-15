# Cosmos data-plane RBAC.
#
# Cosmos has its own role surface (separate from Azure RBAC) because data-
# plane operations need to bypass the control-plane API entirely. The
# built-in "Cosmos DB Built-in Data Contributor" role
# (role definition id 00000000-0000-0000-0000-000000000002) grants
# read/write on items and queries — what the cosmosdbstore needs.
#
# Scope is the SQL database (not the account), so a single Cosmos account
# can host multiple isolated databases each with their own grants.
#
# Callers supply principal IDs (typically Container Apps Managed Identity
# object IDs). Empty list = no assignments, which is appropriate when the
# account uses shared-key auth or principal management lives elsewhere.

locals {
  cosmos_data_contributor_role_id = "00000000-0000-0000-0000-000000000002"

  database_scope = format(
    "%s/dbs/%s",
    azurerm_cosmosdb_account.this.id,
    azurerm_cosmosdb_sql_database.this.name,
  )
}

# count instead of for_each: principal IDs typically come from a Managed
# Identity in a sibling module, which is unknown at plan time. for_each
# requires keys to be known then; count only needs the list length, which
# stays static even when the values inside don't yet exist.
resource "azurerm_cosmosdb_sql_role_assignment" "data_contributor" {
  count = length(var.data_contributor_principal_ids)

  resource_group_name = var.resource_group_name
  account_name        = azurerm_cosmosdb_account.this.name

  role_definition_id = format(
    "%s/sqlRoleDefinitions/%s",
    azurerm_cosmosdb_account.this.id,
    local.cosmos_data_contributor_role_id,
  )

  principal_id = var.data_contributor_principal_ids[count.index]
  scope        = local.database_scope
}
