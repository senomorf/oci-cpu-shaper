# OCI CPU Shaper Overview

The OCI CPU Shaper project provides tools for shaping and orchestrating CPU resource usage across Oracle Cloud Infrastructure workloads. The overarching goal is to offer adaptive scheduling, telemetry integration, and policy-driven controls that help teams right-size compute consumption while maintaining service quality.

This overview summarizes the high-level vision and pointers to additional documentation:

- **IAM and Policies** – Configure dynamic groups and Monitoring permissions so instance principals can query tenancy metrics. See [`01-oci-policy.md`](./01-oci-policy.md).
- **Always Free Guardrails** – Track Oracle’s reclaim thresholds and remediation playbooks. See [`03-free-tier-reclaim.md`](./03-free-tier-reclaim.md).
- **CPU Control Surfaces** – Tune cgroup v2 weights and optional ceilings exposed by container runtimes and Quadlet. See [`04-cgroups-v2.md`](./04-cgroups-v2.md).
- **Monitoring & Alerts** – Issue tenant-signed MQL requests and wire alarms that mirror reclaim detection. See [`05-monitoring-mql.md`](./05-monitoring-mql.md) and [`07-alarms.md`](./07-alarms.md).
- **Contributor Reference** – Explains conventions for extending components in `cmd/`, `pkg/`, and `internal/` while satisfying coverage and lint requirements.

## §5 Configuration and CLI Surfaces

Command-line tooling provides the first entry point for operators. The `shaper` binary exposes the following bootstrap flags:

- `--config` – Absolute or relative path to the primary configuration file. Defaults to `/etc/oci-cpu-shaper/config.yaml`.
- `--log-level` – Structured logging level such as `debug`, `info`, `warn`, or `error`.
- `--mode` – Controller behavior selector. Early builds support `dry-run`, `enforce`, and `noop` placeholders.
  Unsupported values are rejected during flag parsing to keep experiments predictable.

Configuration files follow a layered layout to keep policy inputs and infrastructure wiring distinct:

```yaml
logging:
  level: info
controller:
  mode: dry-run
  refreshInterval: 30s
sources:
  imds:
    endpoint: "https://169.254.169.254/opc/v2"
```

The CLI flags override matching keys in the configuration file, allowing operators to bootstrap with environment defaults and apply targeted overrides for experiments or incident response.

For additional detail on daily CLI usage patterns and option semantics, refer to [`09-cli.md`](./09-cli.md).

Additional documents will be added to detail interfaces, deployment flows, and best practices as the project evolves. For local development environment setup and contributor tooling expectations, see [`08-development.md`](./08-development.md).
