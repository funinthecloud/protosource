locals {
  storage_account_name = (
    var.storage_account_name != ""
    ? var.storage_account_name
    : "${replace(var.name_prefix, "-", "")}state"
  )
}

resource "azurerm_resource_group" "tfstate" {
  name     = "${var.name_prefix}-tfstate-rg"
  location = var.location
  tags     = var.tags
}

# State storage. Versioning + soft-delete + change feed are explicit guards
# against accidental destruction of the only authoritative record of every
# downstream stack.
resource "azurerm_storage_account" "tfstate" {
  name                     = local.storage_account_name
  resource_group_name      = azurerm_resource_group.tfstate.name
  location                 = azurerm_resource_group.tfstate.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  account_kind             = "StorageV2"
  min_tls_version          = "TLS1_2"

  # Disable shared-key access in production; keep it on here so `tofu init`
  # against the backend works without first wiring Managed Identity into
  # every operator's CLI session. Revisit once an automated CI principal
  # is the only writer.
  shared_access_key_enabled = true

  blob_properties {
    versioning_enabled       = true
    change_feed_enabled      = true
    last_access_time_enabled = true

    delete_retention_policy {
      days = 30
    }

    container_delete_retention_policy {
      days = 30
    }
  }

  tags = var.tags
}

resource "azurerm_storage_container" "tfstate" {
  name                  = "tfstate"
  storage_account_id    = azurerm_storage_account.tfstate.id
  container_access_type = "private"
}
