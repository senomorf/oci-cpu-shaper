# §9 Command-Line Interface

The `shaper` binary delivers a thin orchestration layer that connects configuration, logging, and subsystem wiring. Early builds prioritise predictable ergonomics over feature completeness so that operators can familiarise themselves with workflows before controllers are fully implemented.

## 9.1 Invocation

```bash
shaper --config /etc/oci-cpu-shaper/config.yaml --log-level info --mode dry-run
```

The CLI exposes three foundational flags:

| Flag | Description | Default |
| ---- | ----------- | ------- |
| `--config` | Path to the primary YAML configuration file. Relative paths resolve from the current working directory. | `/etc/oci-cpu-shaper/config.yaml` |
| `--log-level` | Structured logging level understood by the Zap logger (`debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`). | `info` |
| `--mode` | Controller operating mode. Placeholder values include `dry-run`, `enforce`, and `noop`; future releases will expand this list as policy features land. | `dry-run` |

Flags are intentionally minimal and map directly to configuration keys so that automation frameworks can template them alongside file-based configuration.

## 9.2 Configuration Layout

Bootstrap deployments can rely on a short YAML manifest:

```yaml
logging:
  level: info
controller:
  mode: dry-run
sources:
  imds:
    endpoint: "https://169.254.169.254/opc/v2"
```

- `logging.level` sets the default log level; the `--log-level` flag overrides it at runtime.
- `controller.mode` selects the shaping strategy; the initial controller is a no-op placeholder that simply reports the requested mode.
- `sources.imds.endpoint` identifies the Oracle Cloud IMDSv2 endpoint and header requirements described in the implementation plan (§5.2).

As additional controllers and data sources ship, this manifest will expand with scheduler parameters, retry policies, and telemetry sinks. Configuration parsing will remain layered so that CLI flags, environment variables, and files compose without surprises.

## 9.3 Diagnostics

At startup the binary emits a structured log line containing build metadata derived from `internal/buildinfo` and echoes the selected mode. This gives operators immediate confirmation of the version, Git commit, and configuration path used for a run before any controllers mutate system state.
