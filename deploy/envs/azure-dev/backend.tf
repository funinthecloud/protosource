# Remote state in the Azure blob container created by deploy/bootstrap.
#
# Run `tofu init -reconfigure` after editing this block, or pass values via
# -backend-config on first init to keep them out of source. Example:
#
#   tofu init \
#     -backend-config="resource_group_name=protosrctf-tfstate-rg" \
#     -backend-config="storage_account_name=protosrctfstate" \
#     -backend-config="container_name=tfstate" \
#     -backend-config="key=azure-dev.tfstate"
#
# The bootstrap module's backend_config_hint output prints the exact values.
terraform {
  backend "azurerm" {
    key = "azure-dev.tfstate"
    resource_group_name  = "protosrctf-tfstate-rg"
    storage_account_name = "protosrctfstate"
    container_name       = "tfstate"
  }
}
