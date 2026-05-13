variable "subscription_id" {
  description = "Azure subscription ID to deploy into. Required so the env doesn't accidentally target whichever subscription `az account show` happens to return."
  type        = string
}

variable "location" {
  description = "Azure region for the dev stack."
  type        = string
  default     = "eastus"
}

variable "name_prefix" {
  description = "Prefix for every resource name in this env. Keep it short and dev-flavored."
  type        = string
  default     = "protosrc-dev"
}

variable "cosmos_account_name" {
  description = "Cosmos DB account name. Must be globally unique. Default appends a short suffix to keep cold-starts collision-free; override when you settle on a name you want to keep."
  type        = string
  default     = ""
}

variable "acr_name" {
  description = "ACR registry name. Must be globally unique, alphanumeric only. Default derives from name_prefix + suffix."
  type        = string
  default     = ""
}

variable "image" {
  description = "Container image to run. Stays on the public Microsoft quickstart until you push your own to the ACR this stack creates."
  type        = string
  default     = "mcr.microsoft.com/k8se/quickstart:latest"
}

variable "tags" {
  description = "Tags applied to all resources in the env."
  type        = map(string)
  default = {
    env       = "dev"
    project   = "protosource"
    managedBy = "tofu"
  }
}
