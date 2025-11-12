variable "tenancy_ocid" {
  description = "OCID of the tenancy that hosts the runner and IAM resources."
  type        = string
}

variable "compartment_ocid" {
  description = "OCID of the compartment where the runner instance and networking resources will be created."
  type        = string
}

variable "region" {
  description = "OCI region identifier (for example, us-phoenix-1)."
  type        = string
}

variable "availability_domain" {
  description = "Optional availability domain name (for example, kIdk:PHX-AD-1). Defaults to the first AD in the region when empty."
  type        = string
  default     = ""
}

variable "shape" {
  description = "Compute shape for the runner. VM.Standard.A1.Flex qualifies for Always Free."
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "shape_ocpus" {
  description = "Number of OCPUs to allocate to the runner instance."
  type        = number
  default     = 2
}

variable "shape_memory_gbs" {
  description = "Amount of memory (GB) assigned to the runner instance."
  type        = number
  default     = 12
}

variable "ubuntu_version" {
  description = "Ubuntu release to use for the runner (Canonical Always Free image)."
  type        = string
  default     = "24.04"
}

variable "ssh_public_key" {
  description = "Public SSH key used to reach the runner for maintenance."
  type        = string
}

variable "admin_cidrs" {
  description = "CIDR blocks that are allowed to reach SSH on the runner."
  type        = list(string)

  validation {
    condition     = length(var.admin_cidrs) > 0
    error_message = "admin_cidrs must include at least one CIDR block."
  }
}

variable "vcn_cidr" {
  description = "CIDR range for the virtual cloud network that hosts the runner."
  type        = string
  default     = "10.10.0.0/24"
}

variable "subnet_cidr" {
  description = "CIDR range for the public subnet used by the runner."
  type        = string
  default     = "10.10.0.0/28"
}

variable "vcn_dns_label" {
  description = "DNS label for the VCN. Must be unique inside the tenancy."
  type        = string
  default     = "ocicpusr"
}

variable "subnet_dns_label" {
  description = "DNS label for the subnet. Must be unique inside the VCN."
  type        = string
  default     = "runner"
}

variable "runner_labels" {
  description = "Additional GitHub Actions labels to advertise on the runner."
  type        = list(string)
  default     = []
}

variable "runner_name" {
  description = "Optional friendly name for the runner. Defaults to oci-cpu-shaper-runner."
  type        = string
  default     = null
}

variable "runner_registration_token" {
  description = "Ephemeral GitHub registration token (PAT exchanges must happen immediately before apply)."
  type        = string
  sensitive   = true
}

variable "runner_service_user" {
  description = "Linux username configured for the GitHub Actions runner service."
  type        = string
  default     = "runner"
}

variable "github_runner_target" {
  description = "GitHub URL to register against (for example, https://github.com/example-org/oci-cpu-shaper)."
  type        = string
}

variable "runner_scope" {
  description = "GitHub registration scope (repo or org)."
  type        = string
  default     = "repo"

  validation {
    condition     = contains(["repo", "org"], var.runner_scope)
    error_message = "runner_scope must be either 'repo' or 'org'."
  }
}

variable "runner_ephemeral" {
  description = "Whether to register the runner in ephemeral mode so each job re-registers."
  type        = bool
  default     = true
}

variable "actions_runner_version" {
  description = "Version of the GitHub Actions runner to install."
  type        = string
  default     = "2.320.0"
}

variable "freeform_tags" {
  description = "Additional freeform tags to apply to the runner instance."
  type        = map(string)
  default     = {}
}

variable "defined_tags" {
  description = "Additional defined tags to apply to the runner instance."
  type        = map(map(string))
  default     = {}
}

variable "iam_tag_namespace" {
  description = "Defined tag namespace used to gate the dynamic group membership."
  type        = string
  default     = null
}

variable "iam_tag_key" {
  description = "Defined tag key used to gate the dynamic group membership."
  type        = string
  default     = null
}

variable "dynamic_group_name" {
  description = "Name of the OCI IAM dynamic group bound to the runner."
  type        = string
  default     = "oci-cpu-shaper-self-hosted"
}

variable "policy_name" {
  description = "Name of the IAM policy that permits Monitoring access for the runner."
  type        = string
  default     = "oci-cpu-shaper-self-hosted-metrics"
}

variable "test_compartment_ocids" {
  description = "List of compartment OCIDs that the runner is allowed to query for Monitoring metrics."
  type        = list(string)

  validation {
    condition     = length(var.test_compartment_ocids) > 0
    error_message = "test_compartment_ocids must not be empty."
  }
}

variable "enable_console_history" {
  description = "Enable capture of console history for the runner instance."
  type        = bool
  default     = false
}

variable "enable_serial_console" {
  description = "Create a serial console connection resource for break-glass access."
  type        = bool
  default     = false
}
