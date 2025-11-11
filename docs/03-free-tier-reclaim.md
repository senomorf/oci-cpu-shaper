# §3 Always Free Reclaim Guardrails

Oracle reclaims idle Always Free compute instances when resource consumption stays below documented thresholds across any rolling seven-day window.[^oci-reclaim] The shaper’s primary objective is to keep P95 CPU usage high enough that production workloads and background maintenance avoid reclaim.

## 3.1 Reclaim criteria

| Signal | Threshold | Notes |
| ------ | --------- | ----- |
| CPU P95 | `< 20%` | Calculated from `CpuUtilization` emitted by the Compute Agent. Raising CPU above the threshold is sufficient even if other criteria fall below 20%. |
| Network | `< 20%` | Applies to public network throughput and is evaluated separately from CPU. |
| Memory | `< 20%` | Only enforced on Ampere A1 shapes. |

The reclaim detector requires **all** three signals to stay below 20% for the entire window before flagging an instance as idle.[^oci-reclaim] Maintaining CPU ≥ 23% gives the controller headroom over transient dips, as outlined in the implementation plan’s goals.

## 3.2 Monitoring CPU utilisation

`docs/05-monitoring-mql.md` describes the `QueryP95CPU` helper and the MQL expression the shaper uses to audit historical performance. When onboarding a new instance:

1. Confirm the Compute Agent plugin is enabled and publishing metrics.
2. Use the Monitoring console or OCI CLI to run the seven-day P95 query.
3. Cross-check values against the shaper’s `/metrics` endpoint to ensure internal telemetry matches Oracle’s readings.

## 3.3 Responding to reclaim notifications

Oracle sends email notifications ahead of reclaim. If alerts cite low CPU utilisation:

- Increase the shaper’s duty-cycle target within the bounds documented in `docs/04-cgroups-v2.md`.
- Verify alarms in `docs/07-alarms.md` triggered appropriately and are routed to an actionable channel.
- Inspect application workloads for throttling or scheduling gaps that keep CPU idle despite the shaper’s background load.

Sustained reclaim warnings warrant a review of deployment knobs and monitoring data to confirm the target instance still requires Always Free capacity.

[^oci-reclaim]: Oracle Cloud Infrastructure, "Always Free Resource Limits". <https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetierlimits.htm#freetierlimits__reclaim>
