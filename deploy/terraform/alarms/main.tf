terraform {
  required_version = ">= 1.5.0"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.5.0"
    }
  }
}

provider "oci" {
  region = var.region
}

locals {
  alarm_display_name = coalesce(var.display_name, "oci-cpu-shaper-p95-guard")
  metric_compartment = coalesce(var.metric_compartment_ocid, var.compartment_ocid)

  alarm_query = "CpuUtilization[1m]{resourceId=\"${var.instance_ocid}\"}.window(7d).percentile(0.95) < 20"

  default_freeform_tags = {
    "oci-cpu-shaper" = "always-free-guardrail"
  }
}

resource "oci_monitoring_alarm" "p95_guardrail" {
  compartment_id       = var.compartment_ocid
  metric_compartment_id = local.metric_compartment
  display_name         = local.alarm_display_name
  namespace            = "oci_computeagent"
  query                = local.alarm_query
  severity             = var.severity
  destinations         = var.notification_topic_ocids
  is_enabled           = var.is_enabled

  freeform_tags = merge(local.default_freeform_tags, var.freeform_tags)
  defined_tags  = var.defined_tags

  body             = var.alarm_body
  pending_duration = var.pending_duration
  resolution       = var.resolution
}
