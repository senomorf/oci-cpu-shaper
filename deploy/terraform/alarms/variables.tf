variable "region" {
  description = "OCI region identifier (for example, us-phoenix-1)."
  type        = string
}

variable "compartment_ocid" {
  description = "Compartment OCID that owns the alarm resource."
  type        = string
}

variable "metric_compartment_ocid" {
  description = "Optional compartment OCID that stores the CpuUtilization metrics. Defaults to compartment_ocid."
  type        = string
  default     = null
}

variable "instance_ocid" {
  description = "Compute instance OCID guarded by the Always Free reclaim alarm."
  type        = string
}

variable "display_name" {
  description = "Optional display name for the alarm."
  type        = string
  default     = null
}

variable "notification_topic_ocids" {
  description = "List of Notification Service topic OCIDs that receive alarm notifications."
  type        = list(string)

  validation {
    condition     = length(var.notification_topic_ocids) > 0
    error_message = "notification_topic_ocids must include at least one topic OCID."
  }
}

variable "severity" {
  description = "Alarm severity reported when the guardrail fires."
  type        = string
  default     = "CRITICAL"
}

variable "is_enabled" {
  description = "Whether the alarm is enabled immediately after creation."
  type        = bool
  default     = true
}

variable "alarm_body" {
  description = "Body text included in alarm notifications."
  type        = string
  default     = "Always Free reclaim guardrail breached. Investigate CpuUtilization and raise the controller duty cycle if required."
}

variable "pending_duration" {
  description = "ISO-8601 duration that CpuUtilization must remain below threshold before firing."
  type        = string
  default     = "PT1H"
}

variable "resolution" {
  description = "Monitoring resolution used by the alarm evaluation window."
  type        = string
  default     = "1m"
}

variable "freeform_tags" {
  description = "Freeform tags merged onto the alarm."
  type        = map(string)
  default     = {}
}

variable "defined_tags" {
  description = "Defined tags merged onto the alarm."
  type        = map(map(string))
  default     = {}
}
