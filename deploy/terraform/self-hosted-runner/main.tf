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
  runner_name   = coalesce(var.runner_name, "oci-cpu-shaper-runner")
  runner_labels = length(var.runner_labels) > 0 ? join(",", var.runner_labels) : "self-hosted,oci-free-tier"

  tag_namespace = coalesce(var.iam_tag_namespace, "OCI_CPU_SHAPER")
  tag_key       = coalesce(var.iam_tag_key, "SelfHostedRunner")

  defined_runner_tags = merge(
    var.defined_tags,
    {
      (local.tag_namespace) = {
        (local.tag_key) = "true"
      }
    },
  )

  freeform_runner_tags = merge(
    {
      "oci-cpu-shaper" = "self-hosted-runner"
    },
    var.freeform_tags,
  )
}

data "oci_identity_availability_domains" "ads" {
  compartment_id = var.tenancy_ocid
}

data "oci_core_images" "ubuntu" {
  compartment_id           = var.compartment_ocid
  operating_system         = "Canonical Ubuntu"
  operating_system_version = var.ubuntu_version
  shape                    = var.shape
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
  state                    = "AVAILABLE"
}

resource "oci_identity_tag_namespace" "runner" {
  compartment_id = var.tenancy_ocid
  description    = "Tags for OCI CPU Shaper self-hosted runners"
  name           = local.tag_namespace

  lifecycle {
    ignore_changes = [defined_tags]
  }
}

resource "oci_identity_tag" "runner" {
  description      = "Marks instances that should join the self-hosted runner dynamic group"
  name             = local.tag_key
  tag_namespace_id = oci_identity_tag_namespace.runner.id
}

resource "oci_core_vcn" "runner" {
  cidr_block     = var.vcn_cidr
  compartment_id = var.compartment_ocid
  display_name   = "oci-cpu-shaper-self-hosted"
  dns_label      = var.vcn_dns_label
}

resource "oci_core_internet_gateway" "runner" {
  compartment_id = var.compartment_ocid
  display_name   = "oci-cpu-shaper-self-hosted-igw"
  vcn_id         = oci_core_vcn.runner.id
  enabled        = true
}

resource "oci_core_route_table" "runner" {
  compartment_id = var.compartment_ocid
  vcn_id         = oci_core_vcn.runner.id
  display_name   = "oci-cpu-shaper-self-hosted-rt"

  route_rules {
    cidr_block        = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.runner.id
  }
}

resource "oci_core_security_list" "runner" {
  compartment_id = var.compartment_ocid
  display_name   = "oci-cpu-shaper-self-hosted-sl"
  vcn_id         = oci_core_vcn.runner.id

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
  }

  dynamic "ingress_security_rules" {
    for_each = var.admin_cidrs
    iterator = cidr

    content {
      description = "Allow SSH from administrator CIDRs"
      protocol    = "6"
      source      = cidr.value

      tcp_options {
        destination_port_range {
          max = 22
          min = 22
        }
      }
    }
  }
}

resource "oci_core_subnet" "runner" {
  cidr_block        = var.subnet_cidr
  compartment_id    = var.compartment_ocid
  display_name      = "oci-cpu-shaper-self-hosted-subnet"
  dns_label         = var.subnet_dns_label
  route_table_id    = oci_core_route_table.runner.id
  security_list_ids = [oci_core_security_list.runner.id]
  vcn_id            = oci_core_vcn.runner.id
  prohibit_public_ip_on_vnic = false
}

resource "oci_core_instance" "runner" {
  availability_domain = var.availability_domain != "" ? var.availability_domain : data.oci_identity_availability_domains.ads.availability_domains[0].name
  compartment_id      = var.compartment_ocid
  display_name        = local.runner_name
  shape               = var.shape

  create_vnic_details {
    assign_public_ip = true
    subnet_id        = oci_core_subnet.runner.id
  }

  shape_config {
    ocpus         = var.shape_ocpus
    memory_in_gbs = var.shape_memory_gbs
  }

  metadata = {
    ssh_authorized_keys = var.ssh_public_key
    user_data           = base64encode(templatefile("${path.module}/templates/cloud-init.yaml.tmpl", {
      github_url          = var.github_runner_target
      runner_name         = local.runner_name
      runner_labels       = local.runner_labels
      github_token        = var.runner_registration_token
      runner_version      = var.actions_runner_version
      configure_ephemeral = var.runner_ephemeral
      github_scope        = var.runner_scope
      admin_username      = var.runner_service_user
    }))
  }

  source_details {
    source_type = "image"
    image_id    = data.oci_core_images.ubuntu.images[0].id
  }

  freeform_tags = local.freeform_runner_tags
  defined_tags  = local.defined_runner_tags

  depends_on = [
    oci_identity_tag_namespace.runner,
    oci_identity_tag.runner,
  ]
}

resource "oci_identity_dynamic_group" "runner" {
  compartment_id = var.tenancy_ocid
  description    = "Self-hosted runner instance principal"
  name           = var.dynamic_group_name

  matching_rule = <<-EOT
ANY {instance.compartment.id = '${var.compartment_ocid}', tag.${local.tag_namespace}.${local.tag_key}.value = 'true'}
EOT

  depends_on = [oci_identity_tag.runner]
}

resource "oci_identity_policy" "runner_metrics" {
  compartment_id = var.tenancy_ocid
  description    = "Allow the self-hosted runner to query Monitoring metrics"
  name           = var.policy_name

  statements = [for compartment in var.test_compartment_ocids :
    "Allow dynamic-group ${var.dynamic_group_name} to read metrics in compartment id ${compartment}"
  ]

  depends_on = [oci_identity_dynamic_group.runner]
}

resource "oci_core_instance_console_history" "runner" {
  count       = var.enable_console_history ? 1 : 0
  instance_id = oci_core_instance.runner.id
}

resource "oci_core_instance_console_connection" "runner" {
  count          = var.enable_serial_console ? 1 : 0
  compartment_id = var.compartment_ocid
  instance_id    = oci_core_instance.runner.id
}
