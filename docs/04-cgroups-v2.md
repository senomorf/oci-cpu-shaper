# §4 Linux cgroup v2 Controls

The shaper relies on cgroup v2 CPU controllers to yield capacity to tenant workloads while maintaining enough baseline activity to avoid Always Free reclaim. Containers and systemd slices expose the same primitives through `cpu.weight` (fair-share) and `cpu.max` (hard ceiling).[^kernel-cpu]

## 4.1 `cpu.weight` for proportional sharing

`cpu.weight` accepts values from 1–10000 and determines how much CPU time the shaper receives relative to other active cgroups. Docker and container runtimes translate `--cpu-shares` into this weight (scaled to the 1–10000 range).[^kernel-cpu-weight] Recommended practices:

- Set a low but non-zero weight (for example, 128) so production workloads preempt the shaper under contention.
- Keep weights consistent across deployments; large swings make tuning difficult and may trigger reclaim due to unpredictable duty cycles.
- Validate runtime mappings after upgrades because past releases of Docker and containerd shipped incorrect v1-to-v2 conversions.[^docker-weight]

The controller observes host load through `/proc/stat` and immediately drops to zero work when contention is detected, so even a modest weight keeps the system responsive.

## 4.2 Optional ceilings via `cpu.max`

`cpu.max` enforces an absolute CPU bandwidth limit by specifying `<quota> <period>`. Leaving the value set to `max` keeps the group work-conserving. Example Quadlet snippet:

```ini
CPUWeight=128
CPUMax=30000 100000
```

This configuration caps the shaper at 30% of one CPU while still allowing bursts when spare capacity exists. Only enable ceilings after validating that the Always Free reclaim alarms remain quiet; aggressive caps can undermine P95 targets.

## 4.3 Observability and troubleshooting

- Inspect `/sys/fs/cgroup/<slice>/cpu.weight` and `/sys/fs/cgroup/<slice>/cpu.max` to confirm runtime configuration.
- Read `/sys/fs/cgroup/<slice>/cpu.stat` for throttling counters and to verify that any configured `cpu.max` value is not hit continuously.
- Pair these checks with the shaper’s telemetry outputs once the `/metrics` endpoint ships (tracked in `docs/ROADMAP.md` §3.1) and with the MQL queries described in `docs/05-monitoring-mql.md`.

Document any new tunables in this file and `docs/CHANGELOG.md` so operators have a single source of truth for CPU control behaviour.

[^kernel-cpu]: The Linux Kernel Documentation, "CPU Controller". <https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html#cpu>
[^kernel-cpu-weight]: The Linux Kernel Documentation, "cpu.weight". <https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html#cpu-interface-files>
[^docker-weight]: GitHub, "containerd/containerd issue #6165: cpu weight conversion for cgroup v2". <https://github.com/containerd/containerd/issues/6165>
