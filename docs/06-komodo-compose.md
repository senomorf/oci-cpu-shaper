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
- `SHAPER_CPU_SHARES` – defaults to `512` to align with rootless Docker engine expectations.
- `SHAPER_MODE`/`SHAPER_LOG_LEVEL` – passed through as CLI arguments.

Launch the stack with:

```bash
docker compose \
  --file deploy/compose/mode-a.rootless.yaml \
  --project-name oci-cpu-shaper \
  up --detach
```

## §6.3 Runtime scripts
Two helper scripts under `deploy/scripts/` wrap `docker run`:

- `run-rootless.sh` pins `--cpu-shares=${SHAPER_CPU_SHARES:-512}` and hardens the container with
  read-only and `no-new-privileges` flags.
- `run-rootful.sh` targets the `rootful` image, retaining the default `--cpu-shares=1024` while
  exposing optional `SHAPER_CPU_PERIOD` and `SHAPER_CPU_QUOTA` overrides for hosts that need
  stricter scheduling control.

Both scripts respect `SHAPER_IMAGE`, `SHAPER_MODE`, `SHAPER_LOG_LEVEL`, and `SHAPER_ENV_FILE` for
consistent execution outside Compose.

## §6.4 Image selection
Use the `oci-cpu-shaper:nonroot` tag for Kubernetes or Docker rootless runtimes. Switch to
`oci-cpu-shaper:rootful` when privileged host integration or cgroup tuning requires UID 0 inside the
container.
