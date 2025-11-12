# Self-Hosted Runner (Always Free)

This Terraform configuration provisions a hardened Ubuntu-based Always Free compute instance that registers itself as a GitHub Actions self-hosted runner. The instance uses instance principal authentication and is restricted to query Monitoring metrics only inside the supplied list of test compartments.

## What the stack creates

- A dedicated VCN, public subnet, internet gateway, route table, and security list scoped to administrator CIDR blocks.
- A `VM.Standard.A1.Flex` instance (Ampere Arm) with Docker, UFW, and the GitHub Actions runner service pre-configured.
- A defined tag namespace/key pair that marks the runner instance and drives a dynamic group.
- A dynamic group and IAM policy that grants `read metrics` against the provided test compartments so the runner can call `QueryP95CPU` using instance principals.

## Prerequisites

1. Install Terraform ≥ 1.5.0 and configure the OCI Terraform provider credentials (CLI config file profile or environment variables).
2. Generate an **ephemeral** GitHub registration token immediately before applying the module. Use `gh api` or the Actions settings UI and set the value via `TF_VAR_runner_registration_token`.
3. Record the following identifiers:
   - Tenancy OCID
   - Compartment OCID that will host the networking resources and compute instance
   - List of compartment OCIDs that host test workloads whose metrics the runner may query
   - Administrator CIDR blocks allowed to reach SSH (for example, a VPN or bastion network)
4. Prepare an SSH key pair dedicated to runner maintenance.

## Usage

```bash
terraform init
terraform apply \
  -var "tenancy_ocid=ocid1.tenancy.oc1..." \
  -var "compartment_ocid=ocid1.compartment.oc1..." \
  -var "region=us-phoenix-1" \
  -var "ssh_public_key=$(cat ~/.ssh/oci-runner.pub)" \
  -var "admin_cidrs=[\"203.0.113.0/24\"]" \
  -var "test_compartment_ocids=[\"ocid1.compartment.oc1.test\"]" \
  -var "github_runner_target=https://github.com/example-org/oci-cpu-shaper" \
  -var "runner_labels=[\"self-hosted\",\"oci-free-tier\"]" \
  -var "runner_registration_token=$TOKEN"
```

Terraform outputs the instance OCID, public IP address, dynamic group OCID, and policy OCID after a successful apply. The cloud-init bootstrap script removes itself once registration succeeds; subsequent token rotations use the maintenance workflow described in `docs/08-development.md`.

## Security hardening notes

- SSH password login is disabled and the root account is locked. Restrict SSH access to administrator CIDR blocks via the `admin_cidrs` variable.
- UFW defaults to deny-all inbound traffic except SSH, matching the security list policy.
- The GitHub Actions runner registers in ephemeral mode by default so each job uses a fresh registration token. Provide `-var "runner_ephemeral=false"` to keep a persistent runner if needed.
- The instance advertises a defined tag (`OCI_CPU_SHAPER.SelfHostedRunner=true` by default) that the dynamic group uses. Do not reuse the tag for non-runner instances.
- Rotate the registration token after each maintenance session and update secrets in GitHub per the maintenance guidance.
