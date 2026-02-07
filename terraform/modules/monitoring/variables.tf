variable "project" {
  type    = string
  default = "sre-platform"
}

variable "environment" {
  type = string
}

variable "oidc_provider_arn" {
  description = "OIDC provider ARN for IRSA"
  type        = string
  default     = ""
}

variable "oidc_provider_url" {
  description = "OIDC provider URL for IRSA"
  type        = string
  default     = ""
}

variable "alert_email" {
  description = "Email for SNS alert notifications"
  type        = string
  default     = ""
}
