# OCI CPU Shaper Overview

The OCI CPU Shaper project provides tools for shaping and orchestrating CPU resource usage across Oracle Cloud Infrastructure workloads. The overarching goal is to offer adaptive scheduling, telemetry integration, and policy-driven controls that help teams right-size compute consumption while maintaining service quality.

This overview summarizes the high-level vision and map to the supporting documentation set:
Operators looking for the fastest onboarding path should start with the [Quick Start](./10-quick-start.md), which condenses the plan-mandated console tasks before diving into the deeper references below.

- **Quick-start (forthcoming)** – Step-by-step bootstrap covering container images, Compose/Quadlet manifests, and smoke tests. This guide will land alongside the release noted in `docs/CHANGELOG.md`.
- **IAM and Policies** – Configure dynamic groups and Monitoring permissions so instance principals can query tenancy metrics. See [`01-oci-policy.md`](./01-oci-policy.md).
- **IMDS Integration** – Understand metadata resolution, retry policies, and offline fallbacks. See [`02-imds-v2.md`](./02-imds-v2.md).
- **Always Free Guardrails** – Track Oracle’s reclaim thresholds and remediation playbooks. See [`03-free-tier-reclaim.md`](./03-free-tier-reclaim.md).
- **CPU Control Surfaces** – Tune cgroup v2 weights and optional ceilings exposed by container runtimes and Quadlet. See [`04-cgroups-v2.md`](./04-cgroups-v2.md).
- **Monitoring & Alerts** – Issue tenant-signed MQL requests and wire alarms that mirror reclaim detection. See [`05-monitoring-mql.md`](./05-monitoring-mql.md) and [`07-alarms.md`](./07-alarms.md).
- **Deployment Patterns** – Compose, Quadlet, and Terraform references that ship Mode A/Mode B defaults. See [`06-komodo-compose.md`](./06-komodo-compose.md).
- **Contributor Reference** – Tooling workflows, coverage expectations, and CI guardrails for extending `cmd/`, `pkg/`, and `internal/`. See [`08-development.md`](./08-development.md) and [`14-ci-pr-workflow-review.md`](./14-ci-pr-workflow-review.md).
- **CLI Reference** – Detailed flag descriptions, configuration layering, and diagnostics. See [`09-cli.md`](./09-cli.md).

## §5 Configuration and CLI Surfaces

Command-line tooling provides the first entry point for operators. The `shaper` binary exposes four bootstrap flags that align with §§3.1 and 5.2 of the implementation plan:

- `--config` – Absolute or relative path to the primary YAML manifest. Defaults to `/etc/oci-cpu-shaper/config.yaml`.
- `--log-level` – Structured logging level recognised by the Zap logger (`debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`).
- `--mode` – Controller behaviour selector. `noop` skips controller wiring for diagnostics, while `dry-run` and `enforce` start the adaptive controller with live OCI metrics when available.
- `--shutdown-after` – Optional duration that cancels the process context after the requested window so smoke tests and diagnostics can exit cleanly.

The CLI also provides `--version` and the equivalent `version` subcommand for fast build metadata checks that avoid configuration loading.

Configuration manifests keep policy inputs and infrastructure wiring distinct. Top-level sections include:

- `controller.*` – Slow-loop targets, relaxed intervals, and suppression thresholds that align with the Always Free reclaim guardrails.
- `estimator.*` – Fast `/proc/stat` sampling cadence that feeds host-load suppression.
- `pool.*` – Worker count and duty-cycle quantum sizing for the CPU load generator.
- `http.*` – Prometheus exporter bind address surfaced at `/metrics`.
- `oci.*` – Compartment OCID, region, optional instance OCID override, and offline toggle used to wire Monitoring clients or static fallbacks.

Environment variables override the YAML manifest so operators can ship the published `configs/mode-a.yaml` and `configs/mode-b.yaml` defaults and apply targeted adjustments for experiments or incident response. The complete CLI and configuration reference lives in [`09-cli.md`](./09-cli.md) and will cross-link to the forthcoming quick-start once it is published.

Additional documents will be added to detail interfaces, deployment flows, and best practices as the project evolves. For local development environment setup and contributor tooling expectations, see [`08-development.md`](./08-development.md).
