# §1 OCI IAM Policy Prerequisites

Instance principals supply the shaper with on-demand access to OCI Monitoring so the controller can adjust CPU targets without embedding user keys.[^oci-iam-overview] Policies and dynamic groups must be provisioned before rolling out the binary.

## 1.1 Dynamic group membership

1. Navigate to **Identity & Security → Dynamic Groups** in the OCI Console.
2. Create a group that captures every compute instance that runs the shaper. A compartment-scoped rule keeps authoring simple:

   ```text
   Any { instance.compartment.id = '<compartment_ocid>' }
   ```
3. Record the group name for use in subsequent policy statements.

Dynamic groups evaluate instance metadata claims in real time, so redeploying or resizing an instance automatically refreshes group membership without additional automation.[^oci-dynamic-groups]

## 1.2 Monitoring read policy

Attach a policy in the home region that grants the dynamic group read access to Monitoring metrics. The minimal statement is:

```text
Allow dynamic-group <group_name> to read metrics in compartment <compartment_name>
```

Scope the policy to the target compartment when possible; use `in tenancy` only when the shaper tracks instances across multiple compartments. `read metrics` maps to the `METRIC_READ` verb needed for the `SummarizeMetricsData` API consumed by `pkg/oci.Client`.[^oci-policies] Update this document and `docs/CHANGELOG.md` whenever new API calls or resource types expand the required permissions.

`pkg/oci.NewInstancePrincipalClient` validates the compartment OCID before constructing the SDK-backed client, so deployments must supply a non-empty compartment identifier at bootstrap time. The returned client shares the policy scope configured here; widening permissions later requires refreshing the binary or configuration to pick up the new compartment target.

## 1.3 Verifying principal access

After applying the policy, confirm that instance principals can authenticate before wiring the controller:

1. SSH into an instance in the dynamic group.
2. Install the OCI CLI and run a simple Monitoring query:

   ```bash
   oci monitoring metric-data summarize-metrics-data \
     --namespace oci_computeagent \
     --query-text "CpuUtilization[1m]{resourceId='<instance_ocid>'}.percentile(0.95)" \
     --compartment-id <compartment_ocid>
   ```
3. A successful response returns JSON datapoints; authorization failures return `NotAuthorizedOrNotFound` errors.

When the shaper binary starts, `pkg/oci` uses the same underlying instance principal flow to obtain temporary credentials, ensuring parity between manual validation and automated queries.

[^oci-iam-overview]: Oracle Cloud Infrastructure, "Ways to Access Oracle Cloud Infrastructure". <https://docs.oracle.com/en-us/iaas/Content/Identity/Concepts/whoisusingoci.htm#ways_access>
[^oci-dynamic-groups]: Oracle Cloud Infrastructure, "Managing Dynamic Groups". <https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/managingdynamicgroups.htm>
[^oci-policies]: Oracle Cloud Infrastructure, "Common Policies". <https://docs.oracle.com/en-us/iaas/Content/Identity/Concepts/commonpolicies.htm>
