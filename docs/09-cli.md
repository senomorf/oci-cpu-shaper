# §9 Command-Line Interface

The `shaper` binary delivers a thin orchestration layer that connects configuration, logging, and subsystem wiring. Early builds prioritise predictable ergonomics over feature completeness so that operators can familiarise themselves with workflows before controllers are fully implemented.

## 9.1 Invocation

```bash
shaper --config /etc/oci-cpu-shaper/config.yaml --log-level info --mode enforce
```

Three foundational flags align with §§3.1 and 5.2 of the implementation plan:

| Flag | Description | Default |
| ---- | ----------- | ------- |
| `--config` | Path to the primary YAML configuration file. Relative paths resolve from the current working directory. | `/etc/oci-cpu-shaper/config.yaml` |
| `--log-level` | Structured logging level understood by the Zap logger (`debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`). | `info` |
| `--mode` | Controller operating mode. `dry-run` and `enforce` now spin up the adaptive controller with real OCI metrics, estimator sampling, and worker pools; `noop` keeps the historical bypass for smoke tests. | `dry-run` |
| `--shutdown-after` | Optional duration that cancels the run context after the requested window, letting CI smoke tests and diagnostics shut down predictably without external supervisors. | `0s` (disabled) |

Flags remain intentionally minimal so orchestration tools can template them alongside file-based configuration and environment overrides. When `--shutdown-after` is non-zero the CLI installs a context deadline and treats the resulting `context deadline exceeded`/`context canceled` errors as clean shutdowns so smoke tests can rely on exit status `0`.

## 9.2 Configuration Layout

Bootstrap deployments rely on a compact YAML manifest that mirrors §§3.1 and 5.2 thresholds:

```yaml
controller:
  targetStart: 0.25
  targetMin: 0.22
  targetMax: 0.40
  stepUp: 0.02
  stepDown: 0.01
  fallbackTarget: 0.25
  interval: 1h
  relaxedInterval: 6h
  relaxedThreshold: 0.28
estimator:
  interval: 1s
pool:
  workers: 4
  quantum: 1ms
http:
  bind: ":9108"
oci:
  compartmentId: "ocid1.compartment.oc1..example"
  region: "us-phoenix-1"
  instanceId: "ocid1.instance.oc1..example"
```

- `controller.*` mirrors the slow-loop thresholds from §3.1, including the one-hour cadence and relaxed six-hour interval when OCI P95 remains healthy.
- `estimator.interval` controls the fast `/proc/stat` sampler cadence (§5.2) while the worker `pool` exposes quantum sizing that stays within the 1–5 ms duty-cycle budget.
- `http.bind` retains the Prometheus listener address, and `oci.compartmentId` supplies the tenancy scope required by the Monitoring client while `oci.region` pins the Monitoring endpoint region when IMDS access is unavailable (for example, CI smoke tests).
- `oci.instanceId` is optional and lets operators bypass IMDS lookups when metadata access is blocked (for example, CI smoke tests or staging environments without instance principals). When `oci.offline` is set the CLI injects a static metrics client and fallback instance ID so dry-run/enforce can exercise the adaptive controller without IMDS or Monitoring access (§§5.2, 11).

When `oci.compartmentId` or `oci.region` are omitted in online deployments the CLI now consults IMDS to resolve both values before constructing the Monitoring client, ensuring metrics queries and structured logs include the canonical tenancy metadata without additional configuration.

Configuration parsing layers file contents with environment overrides so operators can tune production deployments without editing manifests directly.

## 9.3 Environment Overrides

The CLI honours the following environment variables, matching the naming in §5.2:

| Variable | Description | Default |
| -------- | ----------- | ------- |
| `SHAPER_TARGET_START` | Initial duty-cycle target when OCI data is unavailable. | `0.25` |
| `SHAPER_TARGET_MIN` / `SHAPER_TARGET_MAX` | Bounds applied to adaptive adjustments. | `0.22` / `0.40` |
| `SHAPER_STEP_UP` / `SHAPER_STEP_DOWN` | Target deltas when OCI P95 is below or above the goal band. | `+0.02` / `-0.01` |
| `SHAPER_FALLBACK_TARGET` | Fixed target while OCI metrics are unavailable. | `0.25` |
| `SHAPER_SLOW_INTERVAL` / `SHAPER_SLOW_INTERVAL_RELAXED` | Baseline and relaxed controller cadences. | `1h` / `6h` |
| `SHAPER_FAST_INTERVAL` | Host CPU sampling cadence for the estimator. | `1s` |
| `SHAPER_WORKER_COUNT` | Number of duty-cycle workers (`>=1`). | `runtime.NumCPU()` |
| `HTTP_ADDR` | Prometheus listener bind address. | `:9108` |
| `OCI_COMPARTMENT_ID` | Tenancy scope for OCI Monitoring API calls. | *(required for enforce/dry-run unless offline mode is enabled)* |
| `OCI_REGION` | Overrides the Monitoring region, avoiding live IMDS lookups when running in smoke-test environments. | *(empty)* |
| `OCI_INSTANCE_ID` | Overrides the instance OCID used for Monitoring queries and IMDS metadata logs, skipping live metadata calls. | *(empty)* |
| `OCI_OFFLINE` | Enables the static metrics client and metadata fallback described above so smoke tests can bootstrap without IMDS or Monitoring access. | `false` |

Unset or malformed overrides fall back to the defaults shown above.

## 9.4 Diagnostics

At startup the binary emits a structured log line containing build metadata derived from `internal/buildinfo`, the resolved OCI compartment/region pair, and the selected mode. When the shutdown timer is enabled the log also captures the requested duration so operators can confirm the controller will terminate automatically. This gives operators immediate confirmation of the version, Git commit, configuration path, tenancy metadata, and lifecycle expectations before any controllers mutate system state.

Invalid flag values are rejected during argument parsing: unknown controller modes surface an error and cause the program to exit with status `2`, unsupported log levels report a structured error before the logger is constructed, and negative `--shutdown-after` durations are rejected. This keeps early runs predictable while new policy engines are still being prototyped.

Smoke tests introduced in §11 now cover the dependency-injected entrypoint as well as adaptive-controller wiring, ensuring that enforce/dry-run builds start the OCI client, estimator sampler, and worker pool while `noop` preserves the bypass path for validation scenarios. Offline mode keeps this wiring intact by substituting the static metrics client so container smoke tests can run without live tenancy credentials, and new unit coverage exercises the IMDS-backed region/compartment resolver plus its failure modes to keep the ≥85% statement coverage guarantee intact.
