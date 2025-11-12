output "runner_instance_ocid" {
  description = "OCID of the GitHub Actions runner instance."
  value       = oci_core_instance.runner.id
}

output "runner_public_ip" {
  description = "Public IPv4 address assigned to the runner."
  value       = oci_core_instance.runner.public_ip
}

output "dynamic_group_ocid" {
  description = "OCID of the dynamic group bound to the runner."
  value       = oci_identity_dynamic_group.runner.id
}

output "policy_ocid" {
  description = "OCID of the IAM policy granting Monitoring access."
  value       = oci_identity_policy.runner_metrics.id
}
