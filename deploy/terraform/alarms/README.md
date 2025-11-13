# Always Free P95 Alarm

This configuration provisions an OCI Monitoring alarm that enforces the Always Free reclaim guardrail. It evaluates the instance-specific P95 CpuUtilization over a seven-day window and fires when the metric drops below 20% using the following MQL expression:

```text
CpuUtilization[1m]{resourceId="<instance_ocid>"}.window(7d).percentile(0.95) < 20
```

## Inputs

| Variable | Description |
| --- | --- |
| `region` | OCI region identifier (for example, `us-phoenix-1`). |
| `compartment_ocid` | Compartment OCID where the alarm resource is created. |
| `metric_compartment_ocid` | Optional compartment OCID that stores the CpuUtilization metrics. Defaults to `compartment_ocid`. |
| `instance_ocid` | Instance OCID protected by the guardrail. |
| `notification_topic_ocids` | List of Notifications topic OCIDs that should receive alarm events. |
| `display_name` | Optional override for the alarm display name. |
| `severity` | Alarm severity. Defaults to `CRITICAL`. |
| `is_enabled` | Whether the alarm is enabled immediately after creation. Defaults to `true`. |
| `alarm_body` | Notification body. Defaults to a short remediation message. |
| `pending_duration` | ISO-8601 duration that CpuUtilization must remain below threshold before firing. Defaults to `PT1H`. |
| `resolution` | Monitoring resolution to evaluate. Defaults to `1m`. |
| `freeform_tags` | Additional freeform tags. |
| `defined_tags` | Additional defined tags. |

## Outputs

| Output | Description |
| --- | --- |
| `alarm_id` | OCID of the created guardrail alarm. |

## Example

```hcl
module "always_free_alarm" {
  source = "./deploy/terraform/alarms"

  region                 = "us-phoenix-1"
  compartment_ocid       = var.compartment_ocid
  metric_compartment_ocid = var.metric_compartment_ocid
  instance_ocid          = var.instance_ocid
  notification_topic_ocids = [var.guardrail_topic_ocid]
}
```

After applying the module, confirm that the associated Notifications subscription is confirmed so the guardrail can page the on-call rotation. Run `terraform init && terraform apply` from this directory (or a wrapper root module) once the variables are populated to publish the alarm in your tenancy.
