output "alarm_id" {
  description = "OCID of the Always Free reclaim guardrail alarm."
  value       = oci_monitoring_alarm.p95_guardrail.id
}
