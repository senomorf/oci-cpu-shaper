# §10 Quick Start Onboarding

Operators can bring a fresh Oracle Cloud Infrastructure instance under CPU shaping control in five plan-mandated moves. Each step summarises the console action and links to the deep-dive guidance.

## Step 1 — Enable Instance Metrics (§7.1)
Turn on the Compute Instance Monitoring plugin so `CpuUtilization` emits 1-minute samples over the trailing seven days. This unlocks the reclaim guardrail checks that drive adaptive duty cycling. Follow the full walkthrough in [`05-monitoring-mql.md`](./05-monitoring-mql.md).

## Step 2 — Register the Dynamic Group (§7.2)
Create a Dynamic Group that matches the compartment containing the shaper-hosted instances. The group anchors Instance Principals so the controller can authenticate without stored credentials. Detailed policy language and console click-paths live in [`01-oci-policy.md`](./01-oci-policy.md).

## Step 3 — Grant the Metrics Policy (§7.3)
Attach a tenancy policy allowing the Dynamic Group to read Monitoring metrics. The minimal statement is `allow dynamic-group <NAME> to read metrics in tenancy`; scope it further once you confirm telemetry flows. Review the rationale and optional constraints in [`01-oci-policy.md`](./01-oci-policy.md).

## Step 4 — Deploy with Komodo Compose (§6.1)
Roll out the shaper service using the provided Podman Compose or Quadlet examples. Start with the non-root image and `cpu_shares` low enough to yield instantly under contention, matching the fair-share defaults. Sample manifests and mode comparisons are captured in [`06-komodo-compose.md`](./06-komodo-compose.md).

## Step 5 — Wire the Seven-Day Alarm (§7.4)
Create an OCI Monitoring alarm that triggers when the seven-day P95 `CpuUtilization` drops below 20%. Point it at your notification topic so the reclaim risk surfaces before Oracle flags the instance as idle. Step-by-step alarm creation instructions sit in [`07-alarms.md`](./07-alarms.md).
