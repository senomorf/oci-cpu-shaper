# §2 IMDSv2 Integration

The shaper relies on the Oracle Cloud Infrastructure (OCI) Instance Metadata Service (IMDSv2) to discover regional context, instance identity, and hardware shape details. Requests are issued against the link-local endpoint documented by Oracle and use the v2 resource layout to avoid legacy compatibility shims.[^oci-imds]

## 2.1 Endpoints

The client targets the following resources relative to the configured base URL (default: `http://169.254.169.254/opc/v2`):

| Resource | Method | Description |
| -------- | ------ | ----------- |
| `/instance/region` | `GET` | Returns the canonical home region for the running instance as plain text. |
| `/instance/id` | `GET` | Returns the instance OCID as plain text. |
| `/instance/shape-config` | `GET` | Returns a JSON document describing the shape attributes (OCPU count, memory, baseline utilisation, and networking limits). |

The client trims trailing whitespace for text resources and decodes the shape payload into `pkg/imds.ShapeConfig` for downstream consumers. Unknown fields are preserved by the JSON decoder so future metadata additions remain forward compatible.

## 2.2 Retries and timeouts

`pkg/imds` issues requests with a two second client-side timeout and retries up to three times when the metadata service returns retryable status codes (`408`, `429`, or any `5xx` other than `501`). Each retry waits 200 ms before re-issuing the request, honours the provided context for cancellation, and prevents busy loops. These defaults keep the controller responsive while tolerating transient IMDS hiccups and meet the resiliency requirements in §5 of the implementation plan. Override the defaults with `imds.WithMaxAttempts` or `imds.WithBackoff` when integration tests require tighter loops; document any deviations alongside updates to `docs/CHANGELOG.md`. When documenting or extending IMDS behaviour, continue to mirror this policy and cover new paths with unit tests so CI coverage stays above the 85% floor described in §11.

## 2.3 Configuration overrides

`cmd/shaper` reads the optional `OCI_CPU_SHAPER_IMDS_ENDPOINT` environment variable during startup. When set, the binary targets the supplied base URL (for example, a local IMDS emulator used in integration tests); otherwise it falls back to the default link-local endpoint. Operators can also supply `oci.instanceId` in the YAML configuration or `OCI_INSTANCE_ID` via the environment to bypass live metadata calls entirely—useful for CI smoke tests or staged deployments that lack IMDS access. Additional knobs—such as retry budgets or alternative transports—should extend the same environment-variable pattern and must be documented here alongside updates to `docs/CHANGELOG.md`.

[^oci-imds]: Oracle Cloud Infrastructure, "Getting Instance Metadata". <https://docs.oracle.com/en-us/iaas/Content/Compute/Tasks/gettingmetadata.htm>
