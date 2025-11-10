# Project: `oci-cpu-shaper`

## 0) Non-negotiable constraints

* Oracle will mark an Always Free instance “idle” if, during any 7-day window, **all** are true: **P95 CPU < 20%**, **network < 20%**, **memory < 20%** (memory check applies to A1 only). Hitting CPU ≥20% by itself avoids reclaim. ([Oracle Docs][1])
* Use OCI Monitoring metric **`CpuUtilization`** from **`oci_computeagent`**. It is percent of total CPU time, emitted per minute; 1-minute queries return up to 7 days. Use **`percentile(0.95)`** in MQL. ([Oracle Docs][2])
* Use **IMDSv2** only. Endpoints under `/opc/v2/*`, with header `Authorization: Bearer Oracle`. Provide retries per Oracle guidance. ([Oracle Docs][3])
* Linux CPU control is via **cgroup v2**. **`cpu.weight`** controls fair-share distribution. Default weight is 100. Work-conserving when idle. **`cpu.max`** optionally caps bandwidth. ([Linux Kernel Documentation][4])
* IAM must allow the process to **read metrics** using **Instance Principals** (Dynamic Group + “read metrics” policy). ([Oracle Docs][5])

---

## 1) Goals

* Keep 7-day **P95 CPU ≥ 23%** target (headroom over the 20% threshold).
* **Minimal interference**: yield instantly to real workloads; prefer fair-share over capabilities.
* **Containerized**; rootless and rootful both supported. Primary responsiveness lever: **`--cpu-shares`** (cgroup v2 → `cpu.weight`). Optional ceiling via `--cpus`.
* **No fragile knobs**. Safe defaults. Runs months unattended. Low RSS and CPU overhead. Document everything.

---

## 2) Operating modes

We support two documented modes. Rootless is first-class but not exclusive.

* **Mode A: Rootless, fair-share only (default)**

  * Use `--cpu-shares` to set a **low weight** so any contention preempts the shaper.
  * Optionally set `--cpus 0.30` as a hard ceiling. This is off by default but recommended for initial rollout.

* **Mode B: Rootful with best-effort `SCHED_IDLE`**

  * Same as Mode A, plus the process **tries** to set per-thread `SCHED_IDLE`. If it fails, proceed without it.
  * Requires `CAP_SYS_NICE` to succeed. Don’t depend on it. ([man7.org][6])

**Rationale:** `cpu.weight` is enough for responsiveness because cgroup v2 distributes CPU proportionally across *active* groups and is work-conserving when idle. SCHED_IDLE is an optional extra for rootful installs. ([Linux Kernel Documentation][4])

---

## 3) High-level design

### 3.1 Control loops

* **Fast local loop (each 1 s):**

  * Read `/proc/stat` and compute current host CPU busy%.
  * Duty-cycle workers using short quanta (e.g., 1–5 ms busy, sleep remainder) toward a **current target**.
  * If system load is high or runnable tasks detected, drop activity to zero instantly.

* **Slow OCI loop (every 1 h by default, adaptive):**

  * Query MQL over **last 7 days**:
    `CpuUtilization[1m]{resourceId="<instance_ocid>"}.percentile(0.95)`
  * If P95 < 23% → raise target by +2% up to a cap;
    if P95 > 30% → lower −1..−2% (never below 22%).
  * If query fails or returns no data → **fallback mode**: fixed 25% baseline until healthy again.
  * If P95 ≥ 28% for multiple checks → reduce query cadence to every 6 h to save cycles.
  * 1-minute interval is supported with up to 7 days range. ([Oracle Docs][7])

* **Safety alarm (OCI Console):**

  * Alarm expression: `CpuUtilization[1m]{resourceId="<ocid>"}.percentile(0.95) < 20` with a **7-day** window, notify to your email topic. ([Oracle Docs][7])

### 3.2 Metrics and telemetry

* Minimal HTTP `/metrics` in Prometheus text format:

  * `shaper_target_ratio`, `shaper_mode` (normal|fallback), `oci_p95`, `oci_last_success_epoch`, `duty_cycle_ms`, `worker_count`, `host_cpu_percent`.
* No push. No external dependencies.

### 3.3 Instance discovery

* From **IMDSv2** read: `instance/id`, `compartmentId`, `canonicalRegionName`, and `instance/shapeConfig` for `ocpus` if needed for logs. Use retries as Oracle recommends. ([Oracle Docs][3])

### 3.4 OCI access

* Use **Instance Principals**.
* Create a **Dynamic Group** matching your compartment and a **Policy**:
  `allow dynamic-group <DG_NAME> to read metrics in tenancy`
  This grants `METRIC_READ` to call `SummarizeMetricsData`. ([Oracle Docs][5])

---

## 4) Language, build, image

* **Go** 1.23+, static build. Single binary.
* Image: `gcr.io/distroless/static:nonroot` by default; for Mode B publish a `-rootful` image variant that runs as UID 0.
* Target arch: `linux/amd64, linux/arm64`.
* RSS target: ≤ 10–15 MiB steady.

---

## 5) Detailed module plan

### 5.1 Packages

* `pkg/imds`: v2 client.

  * `GetInstance()` → OCID, Compartment OCID, Region.
  * `GetShapeConfig()` → ocpus, memory.
  * Add retry on 404/429/5xx as per doc. ([Oracle Docs][3])
* `pkg/oci`: Monitoring client using Instance Principals.

  * `QueryP95CPU(ocid, last7d)` → float32.
  * Expression uses `CpuUtilization[1m]{resourceId="<ocid>"}.percentile(0.95)`.
  * Handle 7-day time range limits. ([Oracle Docs][7])
* `pkg/est`: `/proc/stat` reader → current host CPU% (1s moving window).
* `pkg/shape`: worker pool and duty cycle logic.

  * Workers spin in short bursts; sleep uses `clock_nanosleep`.
  * **No busy loops** longer than a few ms.
  * If built with `-tags rootful`, call `trySchedIdle()` once; ignore errors. ([man7.org][8])
* `pkg/adapt`: slow controller.

  * State machine: normal, fallback, suppressed.
  * Adaptive interval: 1 h default; 6 h when P95 is comfortably above 28%.
* `pkg/http`: `/metrics` endpoint.
* `cmd/shaper`: wiring, config, logging.

### 5.2 Config (env or flags). Defaults chosen to avoid manual tuning

* `SHAPER_TARGET_START=0.25`
* `SHAPER_TARGET_MIN=0.22`
* `SHAPER_TARGET_MAX=0.40`
* `SHAPER_STEP_UP=0.02`
* `SHAPER_STEP_DOWN=0.01`
* `SHAPER_FAST_INTERVAL=1s`
* `SHAPER_SLOW_INTERVAL=1h`
* `SHAPER_SLOW_INTERVAL_RELAXED=6h`
* `HTTP_ADDR=:9108`
* No region/OCID input needed; IMDSv2 supplies them.

---

## 6) Container guidance

### 6.1 Podman-compose (Komodo) service (Mode A default)

```yaml
services:
  oci-cpu-shaper:
    image: ghcr.io/you/oci-cpu-shaper:0.1
    user: "65532:65532"        # nonroot in distroless
    cpu_shares: 128            # low weight => yields to contention
    # Optional hard cap; leave commented if you want only fair share:
    # cpus: "0.30"
    network_mode: "host"
    environment:
      - SHAPER_TARGET_START=0.25
    restart: unless-stopped
```

### 6.2 Rootful variant that may try SCHED_IDLE (Mode B, optional)

```yaml
services:
  oci-cpu-shaper:
    image: ghcr.io/you/oci-cpu-shaper-rootful:0.1
    user: "0:0"
    cap_add:
      - SYS_NICE            # optional; only for Mode B
    cpu_shares: 128
    # cpus: "0.30"
    network_mode: "host"
    restart: unless-stopped
```

> Keep `cpu_shares` as the main responsiveness lever. `cpus` is optional. In cgroup v2 `cpu.weight` is work-conserving and proportional across active groups; default is 100. ([Linux Kernel Documentation][4])
> Note: runtimes map v1 shares to v2 weight. There have been mapping issues; prefer **small, human-chosen** weights like 128 or 64 rather than relying on “1024 = default”. ([GitHub][9])

### 6.3 Quadlet (user systemd) – same flags map

```
# ~/.config/containers/systemd/oci-cpu-shaper.container
[Unit]
Description=OCI CPU Shaper

[Container]
Image=ghcr.io/you/oci-cpu-shaper:0.1
User=65532:65532
CPUWeight=128
# CPUS=0.30
Network=host
Exec=/shaper --http=:9108
Restart=always

[Install]
WantedBy=default.target
```

---

## 7) OCI setup (Console click-paths)

1. **Enable instance metrics**
   Compute → Instances → your instance → **Metrics**.
   If empty: enable “Compute Instance Monitoring” plugin and ensure public egress or a Service Gateway so the agent can emit to Monitoring. ([Oracle Docs][2])

2. **Dynamic Group**
   Identity & Security → **Dynamic Groups** → Create.
   Rule example: `ALL {instance.compartment.id = "<compartment_ocid>"}`.
   Purpose: allow Instance Principals. ([Oracle Docs][10])

3. **Policy**
   Identity & Security → **Policies** → Create.
   Minimum: `allow dynamic-group <DG_NAME> to read metrics in tenancy`
   This covers `SummarizeMetricsData` (`METRIC_READ`). Optionally scope to `where target.metrics.namespace='oci_computeagent'`. ([Oracle Docs][5])

4. **Alarm**
   Observability & Management → **Alarms** → Create → Advanced mode MQL:
   `CpuUtilization[1m]{resourceId="<INSTANCE_OCID>"}.percentile(0.95) < 20`
   Window: **7 days**. Notification: your Email topic. ([Oracle Docs][7])

5. **IMDSv2**
   Instance → **Instance metadata service** → set v2 only after you confirm the header.
   Endpoints: `http://169.254.169.254/opc/v2/instance/` and `/opc/v2/instance/shapeConfig` with `Authorization: Bearer Oracle`. Implement retries for 404/429/5xx. ([Oracle Docs][3])

---

## 8) E2 vs A1 handling

* The shaper logic is **identical** across shapes because `CpuUtilization` is a **percentage of total time** across the instance. No special logic per OCPU is required. Validate via the alarm. ([Oracle Docs][2])
* A1 has an extra **memory** condition in reclaim policy; we still rely only on CPU. If your actual workloads keep memory well below 20%, CPU ≥ 20% keeps you safe. ([Oracle Docs][1])

---

## 9) Algorithms and pseudocode

### 9.1 Duty-cycle workers

```
every 1s:
  host = readProcStat1s()
  if host.load_high(): set target = 0
  else:
    adjust workers to match current_target
    // busy quantum 1–5 ms, sleep the rest
```

### 9.2 Slow loop

```
timer = SHAPER_SLOW_INTERVAL
while true:
  p95, err = QueryP95CPU(last_7d)
  if err or p95 == 0:
      mode=fallback; current_target = SHAPER_TARGET_START
      timer = SHAPER_SLOW_INTERVAL
  else:
      mode=normal
      if p95 < 0.23: current_target = min(target+STEP_UP, TARGET_MAX)
      if p95 > 0.30: current_target = max(target-STEP_DOWN, TARGET_MIN)
      if p95 > 0.28 consistently: timer = SHAPER_SLOW_INTERVAL_RELAXED
      else: timer = SHAPER_SLOW_INTERVAL
  sleep(timer)
```

**MQL facts used:** `percentile()` is a supported statistic; 1-minute interval returns up to 7 days. ([Oracle Docs][7])

---

## 10) Performance budget

* **CPU**: idle ≤ 0.2% of one core; during shaping, extra CPU = target duty cycle only.
* **Memory**: RSS ≤ 15 MiB.
* **Monitoring**: one HTTP call per slow interval; back off to every 6 h when healthy.

Sampling and emission of `CpuUtilization` are per minute derived from 10-second samples; our 1-hour cadence is more than enough. ([Oracle Docs][2])

---

## 11) Testing

### 11.1 Unit tests

* `/proc/stat` parser under varying jiffy deltas.
* PID scheduler attempt path: success and EPERM paths (behind interface to allow fakes).
* IMDSv2 client: header present, retry policy, JSON mapping.
* MQL client: expression builder, paging, empty/absent data handling.

### 11.2 Integration tests

* Local cgroup v2 slice with two containers: `cpu.weight` of shaper very low; verify competing CPU-bound container preempts shaper under pressure using `cpu.stat` and wallclock. ([Linux Kernel Documentation][4])

### 11.3 E2/A1 regression

* Fake `CpuUtilization` streams to validate controller behavior across 1–4 OCPUs.
* Verify that **7-day P95** logic adjusts targets the right way.

### 11.4 Load tests

* Confirm RSS and duty quantums stay within budget for 24h.

---

## 12) Documentation to ship in repo

* `/docs/00-overview.md` — goals, threat model, non-goals.
* `/docs/01-oci-policy.md` — Dynamic Group + Policy click-paths and example statements, with **links** to Monitoring policy reference. ([Oracle Docs][5])
* `/docs/02-imds-v2.md` — endpoints, headers, retries, examples. ([Oracle Docs][3])
* `/docs/03-free-tier-reclaim.md` — verbatim criteria and links. ([Oracle Docs][1])
* `/docs/04-cgroups-v2.md` — `cpu.weight`, work-conserving behavior, optional `cpu.max`. Explain fair-share and why it preserves responsiveness. ([Linux Kernel Documentation][4])
* `/docs/05-monitoring-mql.md` — how to read 7-day P95 in Service Metrics; example queries and screenshots. ([Oracle Docs][7])
* `/docs/06-komodo-compose.md` — Mode A and B examples, plus Quadlet.
* `/docs/07-alarms.md` — step-by-step alarm creation with advanced MQL. ([Oracle Docs][7])
* `/docs/ROADMAP.md` and `/docs/CHANGELOG.md`.

---

## 13) Roadmap (initial)

* **v0.1** Minimal viable shaper: IMDSv2, MQL P95 controller, fallback baseline, Prometheus metrics, compose examples, alarm doc.
* **v0.2** Adaptive cadence and health heuristics; optional `--cpus` cap examples; E2/A1 validation notes.
* **v0.3** Rootful optional `SCHED_IDLE` attempt path. ([man7.org][8])
* **v0.4** Health endpoints and self-diagnostics (expose last errors).
* **v0.5** Optional Grafana dashboard using OCI metrics and exported `/metrics`.

---

## 14) CI, quality, and release process

* **Linters & static analysis** (low priority but included): `golangci-lint` with vet, errcheck, staticcheck.
* **Tests**: `go test ./...` with race detector on PRs.
* **Releases**: SemVer, multi-arch images via GitHub Actions.
* **SBOM**: `syft` step.
* **Images**: `:latest` and pinned tags.

---

## 15) Example code scaffolding

```
/cmd/shaper/main.go
/pkg/imds/imds.go            // IMDSv2 client with retries
/pkg/oci/monitoring.go       // SummarizeMetricsData wrapper, P95 query
/pkg/est/procstat.go         // /proc/stat sampling
/pkg/shape/worker.go         // duty-cycle engine
/pkg/adapt/controller.go     // target adjustment, modes
/pkg/http/metrics.go         // /metrics handler
/internal/buildinfo/version.go
```

---

## 16) Operational playbook (quick start)

1. **Create Dynamic Group** and **Policy** (“read metrics”) as above. ([Oracle Docs][5])
2. **Enable Monitoring plugin** or ensure egress/Service Gateway. ([Oracle Docs][2])
3. **Run** on each instance with **Mode A** compose shown.
4. **Create 7-day P95 alarm** in Console. ([Oracle Docs][7])
5. After 24–48 h, open **Service Metrics** and verify `CpuUtilization` P95 ≥ 23%. If not, lower `cpu_shares` further or add optional `cpus: "0.30"` until alarm margin is stable.

---

## 17) Design notes and caveats

* **CpuUtilization semantics**: percent of time across the instance. Aggregated at 1-minute from 10-second samples. This percent is independent of OCPU count, so one policy works for E2 and A1. ([Oracle Docs][2])
* **Fair-share math**: `cpu.weight` is proportional among *active* groups and does not waste cycles when others are idle. This keeps the shaper invisible under contention. ([Linux Kernel Documentation][4])
* **Mapping footnote**: some runtimes translate v1 shares to v2 weights. Avoid assuming “1024 is default”; explicitly choose a low weight such as **128**. ([GitHub][9])

---

## 18) What the agent should implement first (MVP checklist)

* IMDSv2 client with retries. ([Oracle Docs][3])
* MQL P95 function call for last 7 days using Instance Principals; parse numeric. ([Oracle Docs][7])
* Duty-cycle workers with 1–5 ms quanta.
* Controller with fallback mode and adaptive cadence.
* `/metrics` endpoint.
* Compose file for Mode A; Quadlet example.
* Unit tests for parser, controller, and error paths.
* Docs: reclaim policy, MQL, IMDSv2, cgroups v2, alarms with links. ([Oracle Docs][1])

This plan satisfies your 12 points while keeping the system simple and safe. If you want me to generate the starter repo structure and initial code skeleton next, say so.

[1]: https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm "Always Free Resources"
[2]: https://docs.oracle.com/en-us/iaas/Content/Compute/References/computemetrics.htm "Compute Instance Metrics"
[3]: https://docs.oracle.com/en-us/iaas/Content/Compute/Tasks/gettingmetadata.htm "Getting Instance Metadata"
[4]: https://docs.kernel.org/admin-guide/cgroup-v2.html "Control Group v2 — The Linux Kernel  documentation"
[5]: https://docs.oracle.com/en-us/iaas/Content/Identity/Reference/monitoringpolicyreference.htm "Details for Monitoring"
[6]: https://man7.org/linux/man-pages/man7/capabilities.7.html?utm_source=chatgpt.com "capabilities(7) - Linux manual page"
[7]: https://docs.oracle.com/en-us/iaas/Content/Monitoring/Reference/mql.htm "Monitoring Query Language (MQL) Reference"
[8]: https://man7.org/linux/man-pages/man7/sched.7.html?utm_source=chatgpt.com "sched(7) - Linux manual page"
[9]: https://github.com/opencontainers/runc/issues/4772?utm_source=chatgpt.com "Conversion of cgroup v1 CPU shares to v2 CPU weight ..."
[10]: https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/managingdynamicgroups.htm?utm_source=chatgpt.com "Managing Dynamic Groups"
