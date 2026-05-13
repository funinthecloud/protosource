locals {
  # ACR requires alphanumeric-only names. Default by stripping hyphens from
  # the prefix and appending 'acr'; callers can override via var.acr_name.
  acr_effective_name = var.acr_name != "" ? var.acr_name : "${replace(var.name_prefix, "-", "")}acr"
}

# Basic SKU is the right default for dev. Upgrade to Premium when private
# endpoints, georeplication, or content trust become real requirements.
resource "azurerm_container_registry" "this" {
  name                = local.acr_effective_name
  resource_group_name = var.resource_group_name
  location            = var.location
  sku                 = "Basic"
  admin_enabled       = false # Managed Identity handles pulls; admin user would be a credential to manage.
  tags                = var.tags
}

# AcrPull on the user-assigned identity, scoped to this registry. The
# Container App's `registry { identity = ... }` block uses this binding
# to authenticate image pulls — no registry credentials live in the app.
resource "azurerm_role_assignment" "acr_pull" {
  scope                = azurerm_container_registry.this.id
  role_definition_name = "AcrPull"
  principal_id         = azurerm_user_assigned_identity.app.principal_id
}

# Key Vault Secrets User on the user-assigned identity, scoped to the vault.
# Only created when secret_refs is in use; otherwise the role assignment
# would dangle without a vault to point at.
resource "azurerm_role_assignment" "kv_secrets_user" {
  count = length(var.secret_refs) > 0 && var.key_vault_id != null ? 1 : 0

  scope                = var.key_vault_id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.app.principal_id
}
