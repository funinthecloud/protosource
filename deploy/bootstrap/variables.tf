variable "location" {
  description = "Azure region for the state storage account."
  type        = string
  default     = "eastus"
}

variable "name_prefix" {
  description = "Short prefix used for the state RG and storage account. Lowercase letters / digits / hyphens, 3-15 chars (storage account adds its own suffix and is capped at 24 chars)."
  type        = string
  default     = "protosrctf"

  validation {
    condition     = can(regex("^[a-z0-9-]{3,15}$", var.name_prefix))
    error_message = "name_prefix must be 3-15 chars of lowercase letters, digits, or hyphens."
  }
}

variable "storage_account_name" {
  description = "Storage account name. Globally unique, 3-24 chars, lowercase alphanumeric only. Falls back to <stripped_prefix>state when empty."
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags applied to all resources."
  type        = map(string)
  default     = {}
}
