# §6 Komodo Compose

## §6.1 Distroless images
The multi-stage [`deploy/Dockerfile`](../deploy/Dockerfile) publishes two distroless targets:
`nonroot` wraps `gcr.io/distroless/static:nonroot` while `rootful` uses the root-enabled
`gcr.io/distroless/static:latest` image. Build metadata is injected with the `VERSION`,
`GIT_COMMIT`, and `BUILD_DATE` build arguments, ensuring `internal/buildinfo` reports accurate
values inside the container.

```bash
docker buildx build \
  --target nonroot \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg GIT_COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t oci-cpu-shaper:nonroot \
  -f deploy/Dockerfile .

docker buildx build \
  --target rootful \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg GIT_COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t oci-cpu-shaper:rootful \
  -f deploy/Dockerfile .
```

## §6.2 Rootless Mode A stack
Compose deployments for Mode A live in `deploy/compose/`. The rootless manifest uses
`mode-a.rootless.yaml` and consumes environment overrides from `${SHAPER_ENV_FILE}` (defaulting to
`deploy/compose/mode-a.env.example`). Key knobs include:

- `SHAPER_IMAGE` – image tag (`oci-cpu-shaper:nonroot` by default).
- `SHAPER_CONFIG_PATH` – host path to mount at `/etc/oci-cpu-shaper/config.yaml`.
- `HTTP_ADDR` – overrides the Prometheus listener bind address (defaults to `:9108` and must match the exposed port below).
- `SHAPER_CPU_SHARES` – defaults to `512` to align with rootless Docker engine expectations.
- `SHAPER_MODE`/`SHAPER_LOG_LEVEL` – passed through as CLI arguments.

Launch the stack with:

```bash
docker compose \
  --file deploy/compose/mode-a.rootless.yaml \
  --project-name oci-cpu-shaper \
  up --detach
```

`mode-a.rootless.yaml` publishes `/metrics` via `${SHAPER_METRICS_BIND:-127.0.0.1:9108}:9108`, so Prometheus scrapes can stay on
the host loopback while the container binds to `HTTP_ADDR` inside the sandbox. Override `SHAPER_METRICS_BIND` when collectors run
outside the host or to expose TLS-terminating sidecars.

## §6.3 Rootful Mode B stack

Operators that need to experiment with the optional Mode B tuning from the
implementation plan (§6.2) can deploy `deploy/compose/mode-b.rootful.yaml`. The
manifest defaults to the `oci-cpu-shaper:rootful` image, runs as UID 0, and adds
`SYS_NICE` so the container can request `SCHED_IDLE` if desired. Runtime knobs
mirror the plan snippet: `cpu_shares` maps to cgroup v2 `cpu.weight` and stays at
`128` by default, while `# cpus` remains commented for hosts that want a hard
quota. Tweak `SHAPER_CAP_SYS_NICE` or `SHAPER_CPUS` in
`deploy/compose/mode-b.env.example` to change those defaults.

The rootful stack pins `network_mode: host` so Prometheus scraping reuses the
node’s address and the container honours whatever `HTTP_ADDR` the binary is
configured with (typically `:9108`). Override `SHAPER_NETWORK_MODE` when a
bridge network is preferable, and adjust `SHAPER_RESTART_POLICY` if the Docker daemon should stop
restarting the container after failures. Rootful builds compiled with
`-tags rootful` now ask the kernel for `SCHED_IDLE` scheduling on each worker
thread as soon as the pool starts (§6.2). The Compose manifest already grants
`SYS_NICE`, which is required to let the kernel honour the request; when the
capability is missing the controller logs `worker failed to enter sched_idle`
at `warn` level and continues without downgrading the scheduler policy.

Bring the Mode B stack up with:

```bash
docker compose \
  --file deploy/compose/mode-b.rootful.yaml \
  --project-name oci-cpu-shaper-mode-b \
  up --detach
```

## §6.4 Quadlet Mode B unit

Podman Quadlet users can apply the same tuning by copying
`deploy/compose/mode-b.rootful.container` into
`~/.config/containers/systemd/`. The unit adds `SYS_NICE`, sets
`CPUWeight=128`, retains the optional `# CPUS=0.30` line for hard caps, and
mounts `/etc/oci-cpu-shaper/config.yaml` read-only by default. After updating
paths and flags to match the target host, enable it with:

```bash
systemctl --user enable --now mode-b.rootful.container
```

## §6.5 Runtime scripts
Two helper scripts under `deploy/scripts/` wrap `docker run`:

- `run-rootless.sh` pins `--cpu-shares=${SHAPER_CPU_SHARES:-512}` and hardens the container with
  read-only and `no-new-privileges` flags, wiring `${HTTP_ADDR:-:9108}` through to the
  container so the `/metrics` listener matches the published port mapping.
- `run-rootful.sh` targets the `rootful` image, retaining the default `--cpu-shares=1024` while
  exposing optional `SHAPER_CPU_PERIOD` and `SHAPER_CPU_QUOTA` overrides for hosts that need
  stricter scheduling control.

Both scripts respect `SHAPER_IMAGE`, `SHAPER_MODE`, `SHAPER_LOG_LEVEL`, and `SHAPER_ENV_FILE` for
consistent execution outside Compose.

## §6.6 Image selection
Use the `oci-cpu-shaper:nonroot` tag for Kubernetes or Docker rootless runtimes. Switch to
`oci-cpu-shaper:rootful` when privileged host integration or cgroup tuning requires UID 0 inside the
container.

## §6.7 Responsiveness verification
Before promoting a new image or Compose bundle, run the CPU weight integration suite described in
[`docs/08-development.md`](08-development.md#-112-cpu-weight-integration-suite). The harness builds
the rootful image, launches a low-weight instance beside a competing CPU-bound container, and asserts
that cgroup v2 honours the expected `cpu.weight` ratios. Capturing the logs (as CI does via
artifacts) provides an audit trail for the responsiveness guarantees promised in §§5 and 9.
