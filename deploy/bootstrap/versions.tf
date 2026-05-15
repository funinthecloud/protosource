# Bootstrap uses LOCAL state intentionally — it provisions the very storage
# account that will hold the remote state for every other env, so it cannot
# depend on a backend that does not yet exist.
#
# Apply this exactly once per Azure subscription. The resulting state file
# (terraform.tfstate) stays on the operator's machine; commit it to a
# private location if you need multi-operator coordination.
terraform {
  required_version = ">= 1.7.0"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }
}

provider "azurerm" {
  features {}
}
