# §9 Command-Line Interface

The `shaper` binary delivers a thin orchestration layer that connects configuration, logging, and subsystem wiring. Early builds prioritise predictable ergonomics over feature completeness so that operators can familiarise themselves with workflows before controllers are fully implemented.

## 9.1 Invocation

```bash
shaper --config /etc/oci-cpu-shaper/config.yaml --log-level info --mode enforce
```

When only build metadata is required, the CLI also exposes a fast-exit flag and
subcommand:

```bash
shaper --version
# {Version:1.2.3 GitCommit:abc123 BuildDate:2024-06-01}

shaper version
```

Both forms print the struct returned by `internal/buildinfo.Current()` without
loading configuration or initialising the logger, keeping diagnostics scripts
and packaging checks lightweight (§5.2).

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
  goalLow: 0.23
  goalHigh: 0.30
  interval: 1h
  relaxedInterval: 6h
  relaxedThreshold: 0.28
  suppressThreshold: 0.85
  suppressResume: 0.70
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

- The repository publishes these defaults as ready-to-use manifests at
  `configs/mode-a.yaml` and `configs/mode-b.yaml`. The Compose and Quadlet
  manifests in §6 mount the matching file so Mode A (rootless) and Mode B
  (rootful) stacks boot with the documented configuration when no overrides are
  supplied.
- `controller.*` mirrors the slow-loop thresholds from §3.1, including the one-hour cadence and relaxed six-hour interval when OCI P95 remains healthy. The fast-loop suppression settings (`suppressThreshold`, `suppressResume`) decide when estimator-driven contention drops the worker pool to zero and when work resumes after the host cools.
- Validation now enforces that every slow-loop target or goal remains below both suppression thresholds, so manifests that would immediately re-trigger the fast loop are rejected with an exit status of `2` and a descriptive error message (§§3.1, 5.2).
- `estimator.interval` controls the fast `/proc/stat` sampler cadence (§5.2) while the worker `pool` exposes quantum sizing that stays within the 1–5 ms duty-cycle budget.
- `http.bind` retains the Prometheus listener address and now backs the `/metrics` exporter described in §9.5, while `oci.compartmentId` supplies the tenancy scope required by the Monitoring client and `oci.region` pins the Monitoring endpoint region when IMDS access is unavailable (for example, CI smoke tests).
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
| `SHAPER_SUPPRESS_THRESHOLD` / `SHAPER_SUPPRESS_RESUME` | Fast-loop suppression thresholds that gate the zero-target mode. | `0.85` / `0.70` |
| `SHAPER_WORKER_COUNT` | Number of duty-cycle workers (`>=1`). | `runtime.NumCPU()` |
| `HTTP_ADDR` | Prometheus listener bind address. | `:9108` |
| `OCI_COMPARTMENT_ID` | Tenancy scope for OCI Monitoring API calls. | *(required for enforce/dry-run unless offline mode is enabled)* |
| `OCI_REGION` | Overrides the Monitoring region, avoiding live IMDS lookups when running in smoke-test environments. | *(empty)* |
| `OCI_INSTANCE_ID` | Overrides the instance OCID used for Monitoring queries and IMDS metadata logs, skipping live metadata calls. | *(empty)* |
| `OCI_OFFLINE` | Enables the static metrics client and metadata fallback described above so smoke tests can bootstrap without IMDS or Monitoring access. | `false` |

Unset or malformed overrides fall back to the defaults shown above.

### Layering overrides

Environment variables sit on top of the YAML file, so operators can mount
`configs/mode-a.yaml` or `configs/mode-b.yaml` verbatim and then tune specific
thresholds without editing the manifest. For example:

```bash
SHAPER_TARGET_START=0.28 SHAPER_SUPPRESS_THRESHOLD=0.90 \
  SHAPER_SUPPRESS_RESUME=0.75 \
  shaper --config /etc/oci-cpu-shaper/config.yaml --mode enforce
```

Compose deployments use the `SHAPER_ENV_FILE` hook described in §6 to inject the
same overrides. Each line follows the shell `KEY=value` syntax, so adding
`SHAPER_TARGET_MAX=0.45` in `deploy/compose/mode-a.env.example` produces the
same runtime effect as exporting the variable directly.

## 9.4 Diagnostics

At startup the binary emits a structured log line containing build metadata derived from `internal/buildinfo`, the resolved OCI compartment/region pair, and the selected mode. The log now also includes `controllerState`, allowing operators to see whether the fast-loop suppression is active when the process initialises. When the shutdown timer is enabled the log also captures the requested duration so operators can confirm the controller will terminate automatically. This gives operators immediate confirmation of the version, Git commit, configuration path, tenancy metadata, suppression status, and lifecycle expectations before any controllers mutate system state.

Invalid flag values are rejected during argument parsing: unknown controller modes surface an error and cause the program to exit with status `2`, unsupported log levels report a structured error before the logger is constructed, and negative `--shutdown-after` durations are rejected. This keeps early runs predictable while new policy engines are still being prototyped.

Configuration validation shares this behaviour: when thresholds conflict with the suppression bounds the CLI prints the descriptive failure and exits with code `2`, preventing partially initialised controllers (§§3.1, 5.2).

Smoke tests introduced in §11 now cover the dependency-injected entrypoint as well as adaptive-controller wiring, ensuring that enforce/dry-run builds start the OCI client, estimator sampler, and worker pool while `noop` preserves the bypass path for validation scenarios. Offline mode keeps this wiring intact by substituting the static metrics client so container smoke tests can run without live tenancy credentials, and new unit coverage exercises the IMDS-backed region/compartment resolver plus its failure modes to keep the ≥85% statement coverage guarantee intact.

Rootful binaries built with `-tags rootful` log a warning if the kernel rejects the
`SCHED_IDLE` request emitted when the worker pool starts (§§6, 9). Hosts running
the Compose or Quadlet stacks must grant `CAP_SYS_NICE`/`SYS_NICE` so the
`worker failed to enter sched_idle` warning remains informational rather than a
permanent indicator that the downgrade could not be applied.

## 9.5 Metrics Exporter

`cmd/shaper` instantiates the lightweight OpenMetrics exporter from `pkg/http/metrics` and serves it at `/metrics` using the `http.bind` configuration (or `HTTP_ADDR` environment override). The listener defaults to `:9108`, matching the Compose port mapping in §6 and the container `EXPOSE 9108` declaration. Production Prometheus servers can scrape the endpoint directly when the rootful stack runs in host-network mode, while rootless deployments forward `${SHAPER_METRICS_BIND:-127.0.0.1:9108}:9108` from the host loopback to the container port.

### Emitted series

| Metric | Type | Description |
| ------ | ---- | ----------- |
| `shaper_target_ratio` | gauge | Current duty-cycle target assigned to the worker pool (0.0–1.0). |
| `shaper_mode{mode="<name>"}` | gauge | Active controller mode (`noop`, `dry-run`, or `enforce`) reported as a labelled one-hot gauge. |
| `shaper_state{state="<name>"}` | gauge | Controller state-machine output (`normal`, `fallback`, `suppressed`, or `unknown`). |
| `oci_p95` | gauge | Latest OCI `CpuUtilization` P95 ratio used for adaptive decisions. |
| `oci_last_success_epoch` | counter | Unix epoch seconds when `QueryP95CPU` last succeeded (`0` while offline). |
| `duty_cycle_ms` | gauge | Worker quantum configured for each duty-cycle interval in milliseconds. |
| `worker_count` | gauge | Number of goroutines currently driving CPU load. |
| `host_cpu_percent` | gauge | Most recent host CPU utilisation sample from the fast estimator loop. |

### Example scrape output

```
# HELP shaper_target_ratio Target duty cycle ratio assigned to worker pool.
# TYPE shaper_target_ratio gauge
shaper_target_ratio 0.275000
# HELP shaper_mode Controller operating mode (value set to 1 for the active mode).
# TYPE shaper_mode gauge
shaper_mode{mode="dry-run"} 1
# HELP shaper_state Controller state machine output (value set to 1 for the active state).
# TYPE shaper_state gauge
shaper_state{state="fallback"} 1
# HELP oci_p95 Last observed OCI CPU P95 ratio.
# TYPE oci_p95 gauge
oci_p95 0.180000
# HELP oci_last_success_epoch Unix epoch seconds of the last successful OCI metrics query.
# TYPE oci_last_success_epoch counter
oci_last_success_epoch 0
# HELP duty_cycle_ms Duty cycle quantum configured for workers (milliseconds).
# TYPE duty_cycle_ms gauge
duty_cycle_ms 1.000
# HELP worker_count Number of worker goroutines consuming CPU.
# TYPE worker_count gauge
worker_count 4
# HELP host_cpu_percent Last recorded host CPU utilisation percentage.
# TYPE host_cpu_percent gauge
host_cpu_percent 6.25
# EOF
```

Offline mode continues to populate each series so smoke tests and container health checks can rely on the exporter without live tenancy credentials; only `oci_last_success_epoch` remains `0` until Monitoring calls succeed. Unit and CLI tests exercise the handler through `httptest.Server`, preserving the ≥85% coverage floor mandated in §11.
