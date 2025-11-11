# §5 Monitoring & MQL

Monitoring data informs the shaper’s adaptive policy and must be retrievable from the tenant without manual credential handling. The controller issues Monitoring Query Language (MQL) requests through instance principals so the runtime binary can operate without embedding user API keys.[^oci-monitoring-auth] Alarm wiring that mirrors these queries is documented in [`07-alarms.md`](./07-alarms.md).

## 5.1 Instance principal metrics access

1. Place every compute instance that runs the shaper into a dynamic group (for example, `Any { instance.compartment.id = '<compartment_ocid>' }`).
2. Attach a policy granting read-only Monitoring access to that group, such as:

   ```text
   Allow dynamic-group cpu-shaper to read metrics in compartment <compartment_name>
   ```

3. Ensure the instances have outbound access to the regional `telemetry` endpoint documented by Oracle so the SummarizeMetricsData API succeeds.[^oci-monitoring-endpoint]

The Go SDK automatically exchanges the instance principal token set for a temporary keypair and signs each Monitoring request. No additional configuration files are necessary on the host.

## 5.2 Querying CpuUtilization P95

`pkg/oci.Client.QueryP95CPU` sends `SummarizeMetricsData` requests using the following MQL expression:

```text
CpuUtilization[1m]{resourceId = "<instance_ocid>"}.percentile(0.95)
```

The method wraps the OCI Go SDK client, paginates over `opc-next-page` tokens, and selects the most recent aggregated datapoint. When the caller requests a seven-day lookback the query automatically truncates the interval to the Monitoring service’s one-minute resolution ceiling so the API never rejects the call.[^oci-monitoring-mql] The helper returns `ErrNoMetricsData` when no datapoints are available, allowing the controller to fall back to on-host estimators. Unit tests exercise pagination, empty result handling, and the exact query string via an HTTP-backed mock to preserve the ≥85% coverage floor mandated in §11.

## 5.3 Troubleshooting

- **`ErrNoMetricsData`** – Verify that the instance publishes `CpuUtilization` metrics (enabled by the Compute Agent) and that the queried window contains traffic. Check the Monitoring console for gaps or disablement in the agent plugin.[^oci-compute-agent]
- **HTTP 401/403 responses** – Confirm the instance belongs to the dynamic group referenced by the policy and that the policy grants `read metrics` on the target compartment.
- **HTTP 429/5xx responses** – The helper wraps the raw error so controllers can trigger retries or fall back to cached data. Validate regional connectivity and consider enabling per-request retry logic before escalating.

[^oci-monitoring-auth]: Oracle Cloud Infrastructure, "Ways to Access Oracle Cloud Infrastructure". <https://docs.oracle.com/en-us/iaas/Content/Identity/Concepts/whoisusingoci.htm#ways_access>
[^oci-monitoring-endpoint]: Oracle Cloud Infrastructure, "Monitoring Endpoints". <https://docs.oracle.com/en-us/iaas/Content/Monitoring/Concepts/monitoringoverview.htm#endpoints>
[^oci-monitoring-mql]: Oracle Cloud Infrastructure, "Monitoring Query Language (MQL) Reference". <https://docs.oracle.com/en-us/iaas/Content/Monitoring/Reference/mql.htm>
[^oci-compute-agent]: Oracle Cloud Infrastructure, "Enabling Compute Agent Plugins". <https://docs.oracle.com/en-us/iaas/Content/Compute/Tasks/manage-plugins.htm>
