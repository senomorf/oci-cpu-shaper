# OCI CPU Shaper Overview

The OCI CPU Shaper project provides tools for shaping and orchestrating CPU resource usage across Oracle Cloud Infrastructure workloads. The overarching goal is to offer adaptive scheduling, telemetry integration, and policy-driven controls that help teams right-size compute consumption while maintaining service quality.

This overview summarizes the high-level vision and pointers to additional documentation:

- **Architecture and Services** – Describes how command-line tooling, shared packages, and deployment assets fit together. (See forthcoming documents in the `docs/` directory.)
- **Operational Guidance** – Covers build metadata, configuration options, and integration points with OCI metadata services.
- **Contributor Reference** – Explains conventions for extending components in `cmd/`, `pkg/`, and `internal/`.

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
