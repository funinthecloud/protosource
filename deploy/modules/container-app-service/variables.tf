variable "resource_group_name" {
  description = "Existing resource group. The module does not create the resource group."
  type        = string
}

variable "location" {
  description = "Azure region (e.g. eastus, westus2)."
  type        = string
}

variable "name_prefix" {
  description = "Short prefix used for all resource names. Lowercase letters / digits / hyphens; 3-20 chars. Composed names (ACR, etc.) must fit within their per-resource length limits."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]{3,20}$", var.name_prefix))
    error_message = "name_prefix must be 3-20 chars of lowercase letters, digits, or hyphens."
  }
}

variable "acr_name" {
  description = "ACR registry name. Must be globally unique, 5-50 chars, alphanumeric only (no hyphens). Falls back to name_prefix with hyphens stripped + 'acr' suffix when empty."
  type        = string
  default     = ""
}

variable "image" {
  description = "Container image to deploy. Defaults to the Microsoft quickstart so the first `tofu apply` succeeds before the consumer has pushed their own image to ACR. Replace via `tofu apply -var image=...` once the real image is available."
  type        = string
  default     = "mcr.microsoft.com/k8se/quickstart:latest"
}

variable "target_port" {
  description = "Container listen port. The Go runtime defaults to 8080 (see cmd/testcosmos)."
  type        = number
  default     = 8080
}

variable "env" {
  description = "Plain environment variables passed to the container. For sensitive values use secret_refs instead so the value never appears in the Container App configuration."
  type        = map(string)
  default     = {}
}

variable "secret_refs" {
  description = "Key Vault-backed secrets. Map of secret_name => key_vault_secret_id (the full versioned or unversioned secret URI). The Container App pulls these via the module's Managed Identity at runtime; they surface inside the container as environment variables named after the map key, upper-cased and hyphens-replaced-with-underscores."
  type        = map(string)
  default     = {}
}

variable "key_vault_id" {
  description = "Key Vault resource ID. Required when secret_refs is non-empty so the module can grant the Managed Identity Key Vault Secrets User on the vault. Pass null when not using secret_refs."
  type        = string
  default     = null
}

variable "min_replicas" {
  description = "Minimum replicas. 0 enables scale-to-zero (free when idle but adds a cold-start)."
  type        = number
  default     = 0
}

variable "max_replicas" {
  description = "Maximum replicas. Container Apps caps each environment at 1000."
  type        = number
  default     = 3
}

variable "cpu" {
  description = "vCPU per replica. Container Apps requires CPU/memory to be on the supported matrix (e.g. 0.25/0.5Gi, 0.5/1Gi, 1/2Gi)."
  type        = number
  default     = 0.25
}

variable "memory" {
  description = "Memory per replica. Must pair with cpu per the supported matrix."
  type        = string
  default     = "0.5Gi"
}

variable "log_analytics_workspace_id" {
  description = "Existing Log Analytics workspace resource ID. When null the module creates a small per-deployment workspace; for multi-app environments, share one workspace across modules to consolidate logs and reduce cost."
  type        = string
  default     = null
}

variable "tags" {
  description = "Tags applied to all resources the module creates."
  type        = map(string)
  default     = {}
}
