locals {
  # Convert secret_refs map keys ("cosmos-key") into env-var-safe names
  # ("COSMOS_KEY"). The Container App secret name itself keeps the original
  # form because Container Apps requires lowercase/hyphenated secret names.
  secret_env_pairs = {
    for k, _ in var.secret_refs :
    upper(replace(k, "-", "_")) => k
  }

  # AZURE_CLIENT_ID disambiguates which user-assigned identity
  # DefaultAzureCredential should request from IMDS. Without it, IMDS
  # returns 400 "Unable to load the proper Managed Identity" whenever the
  # container has any user-assigned identities attached. Merged here (not
  # at the env call site) because the identity is created inside this
  # module — wiring it through var.env would be a self-reference cycle.
  managed_env = merge(var.env, {
    AZURE_CLIENT_ID = azurerm_user_assigned_identity.app.client_id
  })
}

# The Container App itself. Uses the user-assigned identity for both ACR
# pulls (registry block) and Key Vault secret fetches (secret blocks).
#
# `secret` blocks with key_vault_secret_id pull live values from Key Vault
# at startup — the secret never lands in the Container App's exported
# configuration, which is the whole point of going through KV.
resource "azurerm_container_app" "this" {
  name                         = "${var.name_prefix}-app"
  resource_group_name          = var.resource_group_name
  container_app_environment_id = azurerm_container_app_environment.this.id
  revision_mode                = "Single"

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.app.id]
  }

  registry {
    server   = azurerm_container_registry.this.login_server
    identity = azurerm_user_assigned_identity.app.id
  }

  dynamic "secret" {
    for_each = var.secret_refs
    content {
      name                = secret.key
      key_vault_secret_id = secret.value
      identity            = azurerm_user_assigned_identity.app.id
    }
  }

  template {
    min_replicas = var.min_replicas
    max_replicas = var.max_replicas

    container {
      name   = "app"
      image  = var.image
      cpu    = var.cpu
      memory = var.memory

      dynamic "env" {
        for_each = local.managed_env
        content {
          name  = env.key
          value = env.value
        }
      }

      # Each Key Vault secret surfaces inside the container as an env var
      # whose name is the upper-snake-cased map key. Container Apps wires
      # the secret_name reference; the runtime never sees the KV URL.
      dynamic "env" {
        for_each = local.secret_env_pairs
        content {
          name        = env.key
          secret_name = env.value
        }
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = var.target_port
    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }

  tags = var.tags

  # The AcrPull and Key Vault role assignments must be live before the
  # Container App tries to pull / read secrets on first revision creation.
  depends_on = [
    azurerm_role_assignment.acr_pull,
    azurerm_role_assignment.kv_secrets_user,
  ]
}
