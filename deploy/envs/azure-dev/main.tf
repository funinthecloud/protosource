# =============================================================================
# Cold-start from a fresh Azure subscription
# =============================================================================
#
# 1. az login
#    az account set --subscription <your-subscription-id>
#
# 2. One-shot bootstrap of the state backend (local state, ~ a minute):
#      cd deploy/bootstrap
#      tofu init
#      tofu apply
#    Note the three output values (resource_group_name, storage_account_name,
#    container_name) — you'll pass them to the env init below.
#
# 3. Initialize this env against the bootstrap-provisioned backend:
#      cd ../envs/azure-dev
#      cp example.tfvars terraform.tfvars   # then edit subscription_id
#      tofu init \
#        -backend-config="resource_group_name=<bootstrap rg>" \
#        -backend-config="storage_account_name=<bootstrap sa>" \
#        -backend-config="container_name=tfstate"
#
# 4. Plan + apply:
#      tofu plan
#      tofu apply
#    First apply takes ~5–10 minutes. The Container App boots on the
#    Microsoft quickstart image until you push your own.
#
# 5. Push the testcosmos image (after apply prints acr_login_server):
#      az acr login --name <acr_login_server>
#      docker build -f cmd/testcosmos/Dockerfile -t <acr_login_server>/testcosmos:latest .
#      docker push <acr_login_server>/testcosmos:latest
#      tofu apply -var image=<acr_login_server>/testcosmos:latest
#
# =============================================================================

# Short hex suffix appended to globally-unique resource names (Cosmos
# account, ACR) so a clone-and-apply doesn't collide with an existing
# Azure tenant. The suffix is derived deterministically from
# subscription_id + name_prefix so it's stable across applies and
# operators on the same env — no third-party provider required.
locals {
  suffix = substr(md5("${var.subscription_id}-${var.name_prefix}"), 0, 6)

  cosmos_account_name = (
    var.cosmos_account_name != ""
    ? var.cosmos_account_name
    : "${var.name_prefix}-cosmos-${local.suffix}"
  )

  acr_name = (
    var.acr_name != ""
    ? var.acr_name
    : "${replace(var.name_prefix, "-", "")}acr${local.suffix}"
  )
}

resource "azurerm_resource_group" "this" {
  name     = "${var.name_prefix}-rg"
  location = var.location
  tags     = var.tags
}

# Container App + identity + ACR + log analytics. Standing it up first
# gives us the principal_id that cosmos-eventstore needs to grant
# data-plane access to.
module "app" {
  source = "../../modules/container-app-service"

  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location
  name_prefix         = var.name_prefix
  acr_name            = local.acr_name
  image               = var.image
  target_port         = 8080

  env = {
    COSMOS_ENDPOINT               = module.cosmos.endpoint
    COSMOS_USE_DEFAULT_CREDENTIAL = "1"
    COSMOS_DATABASE               = module.cosmos.database_name
    EVENTS_CONTAINER              = module.cosmos.events_container_name
    AGGREGATES_CONTAINER          = module.cosmos.aggregates_container_name
  }

  tags = var.tags
}

# Cosmos account + DB + 2 containers + data-plane RBAC granted to the
# Container App's Managed Identity. Public network access stays on for
# dev — production envs should pin it off and add a Private Endpoint.
module "cosmos" {
  source = "../../modules/cosmos-eventstore"

  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location
  account_name        = local.cosmos_account_name
  database_name       = "protosource"

  data_contributor_principal_ids = [module.app.principal_id]

  tags = var.tags
}
