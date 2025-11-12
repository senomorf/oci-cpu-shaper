# §7 Monitoring Alarms

Always Free instances require early warning when CPU utilisation drifts toward reclaim thresholds. OCI Alarms provide managed notifications based on the same MQL queries the shaper consumes.[^oci-alarms]

## 7.1 Alarm expression

Create an alarm in **Observability & Management → Alarms** using Advanced mode with the following parameters:

```text
CpuUtilization[1m]{resourceId="<instance_ocid>"}.percentile(0.95) < 20
```

- **Window:** `7d` (seven days) to match Oracle’s reclaim evaluation period.
- **Pending duration:** `1h` balances responsiveness against transient dips; tighten as comfort grows.
- **Destinations:** Email or PagerDuty topics reachable by the on-call team.

The alarm should target the exact instance OCID retrieved from IMDSv2 so notifications stay scoped to each node.[^oci-mql]

## 7.2 Routing and testing

1. Confirm the notification topic is subscribed before enabling the alarm.
2. Use the **Test Alarm** feature to emit a sample payload and verify downstream automation.
3. After deployment, compare alarm history with the shaper’s `/metrics` and the `QueryP95CPU` output described in `docs/05-monitoring-mql.md` to ensure data parity.
4. Hit the Prometheus endpoint directly (`curl -fsS ${HTTP_ADDR:-http://127.0.0.1:9108}/metrics`) and confirm the `shaper_mode`, `shaper_state`, and `oci_p95` samples line up with the alarm evaluation window; §9.5 includes a canonical scrape example.

## 7.3 Operational playbook

- **Sustained alerts:** Increase the controller’s duty-cycle target (see `docs/04-cgroups-v2.md`) or investigate workload regressions that keep CPU idle.
- **Missing data incidents:** OCI Alarms treat absent metrics as breaching the condition. Validate Compute Agent health and network reachability, then consult §5.3 of `docs/05-monitoring-mql.md` for troubleshooting steps.
- **Noisy alerts:** Adjust the pending duration or add a secondary alarm tracking network utilisation when reclaim warnings cite multiple signals.

Document any future alarm templates or automation hooks here and in `docs/CHANGELOG.md` so operators can keep policies aligned with production usage.

[^oci-alarms]: Oracle Cloud Infrastructure, "Overview of Alarms". <https://docs.oracle.com/en-us/iaas/Content/Monitoring/Tasks/workingalarms.htm>
[^oci-mql]: Oracle Cloud Infrastructure, "Monitoring Query Language (MQL) Reference". <https://docs.oracle.com/en-us/iaas/Content/Monitoring/Reference/mql.htm>
