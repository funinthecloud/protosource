output "resource_group_name" {
  description = "Resource group holding the state storage. Reference from env backend.tf."
  value       = azurerm_resource_group.tfstate.name
}

output "storage_account_name" {
  description = "State storage account name."
  value       = azurerm_storage_account.tfstate.name
}

output "container_name" {
  description = "Blob container holding state files."
  value       = azurerm_storage_container.tfstate.name
}

# Convenience: paste this into the env's backend.tf (one block per env,
# distinguished by `key`). The actual values land in your shell history so
# treat them as semi-public, not secret.
output "backend_config_hint" {
  description = "Copy-paste snippet for an env's backend block."
  value       = <<-EOT
    terraform {
      backend "azurerm" {
        resource_group_name  = "${azurerm_resource_group.tfstate.name}"
        storage_account_name = "${azurerm_storage_account.tfstate.name}"
        container_name       = "${azurerm_storage_container.tfstate.name}"
        key                  = "<env-name>.tfstate"
      }
    }
  EOT
}
