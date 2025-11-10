#!/usr/bin/env bash
set -euo pipefail

: "${SHAPER_IMAGE:=oci-cpu-shaper:rootful}"
: "${SHAPER_CONTAINER_NAME:=oci-cpu-shaper-root}"
: "${SHAPER_CONFIG_PATH:=./configs/mode-a.yaml}"
: "${SHAPER_MODE:=dry-run}"
: "${SHAPER_LOG_LEVEL:=info}"
: "${SHAPER_CPU_SHARES:=1024}"
: "${SHAPER_CPU_PERIOD:=}"
: "${SHAPER_CPU_QUOTA:=}"
: "${SHAPER_ENV_FILE:=}"

run_args=(
  --rm
  --name "${SHAPER_CONTAINER_NAME}"
  --volume "${SHAPER_CONFIG_PATH}:/etc/oci-cpu-shaper/config.yaml:ro"
  --cpu-shares "${SHAPER_CPU_SHARES}"
)

if [[ -n "${SHAPER_CPU_PERIOD}" ]]; then
  run_args+=(--cpu-period "${SHAPER_CPU_PERIOD}")
fi

if [[ -n "${SHAPER_CPU_QUOTA}" ]]; then
  run_args+=(--cpu-quota "${SHAPER_CPU_QUOTA}")
fi

if [[ -n "${SHAPER_ENV_FILE}" ]]; then
  run_args+=(--env-file "${SHAPER_ENV_FILE}")
fi

run_args+=(
  "${SHAPER_IMAGE}"
  --config /etc/oci-cpu-shaper/config.yaml
  --mode "${SHAPER_MODE}"
  --log-level "${SHAPER_LOG_LEVEL}"
)

exec docker run "${run_args[@]}"
