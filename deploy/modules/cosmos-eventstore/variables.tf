variable "resource_group_name" {
  description = "Existing resource group to deploy into. The module does not create the resource group."
  type        = string
}

variable "location" {
  description = "Azure region for the Cosmos account (e.g. eastus, westus2)."
  type        = string
}

variable "account_name" {
  description = "Cosmos DB account name. Must be globally unique, 3-44 chars, lowercase letters / digits / hyphens."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]{3,44}$", var.account_name))
    error_message = "account_name must be 3-44 chars of lowercase letters, digits, or hyphens."
  }
}

variable "database_name" {
  description = "SQL database id."
  type        = string
  default     = "protosource"
}

variable "events_container_name" {
  description = "Events container id. Mirrors cosmosdbstore.DefaultEventsContainer."
  type        = string
  default     = "events"
}

variable "aggregates_container_name" {
  description = "Aggregates container id. Mirrors cosmosdbstore.DefaultAggregatesContainer."
  type        = string
  default     = "aggregates"
}

variable "serverless" {
  description = "Use the Serverless capability (no provisioned RU/s). Recommended for dev and low-traffic workloads. Toggle off + set provisioned_throughput for production."
  type        = bool
  default     = true
}

variable "provisioned_throughput" {
  description = "Database-level RU/s when serverless = false. Ignored under serverless. Set to a positive integer (e.g. 400) for manual throughput; the module does not currently expose autoscale."
  type        = number
  default     = 0
}

variable "consistency_level" {
  description = "Default account consistency level. Session is the right default for event sourcing: writers see their own writes, readers see monotonic ordering within a session."
  type        = string
  default     = "Session"

  validation {
    condition     = contains(["Strong", "BoundedStaleness", "Session", "ConsistentPrefix", "Eventual"], var.consistency_level)
    error_message = "consistency_level must be one of: Strong, BoundedStaleness, Session, ConsistentPrefix, Eventual."
  }
}

variable "public_network_access_enabled" {
  description = "Allow public network access to the account. Production deployments should set this false and layer a Private Endpoint."
  type        = bool
  default     = true
}

variable "data_contributor_principal_ids" {
  description = "AAD principal IDs (typically Managed Identities of the Container Apps service) that should be granted Cosmos DB Built-in Data Contributor at the database scope. Empty list disables the role assignments — useful when access is managed elsewhere."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Tags applied to the Cosmos account."
  type        = map(string)
  default     = {}
}
