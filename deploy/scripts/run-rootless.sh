#!/usr/bin/env bash
set -euo pipefail

: "${SHAPER_IMAGE:=oci-cpu-shaper:nonroot}"
: "${SHAPER_CONTAINER_NAME:=oci-cpu-shaper}"
: "${SHAPER_CONFIG_PATH:=./configs/mode-a.yaml}"
: "${SHAPER_MODE:=dry-run}"
: "${SHAPER_LOG_LEVEL:=info}"
: "${SHAPER_CPU_SHARES:=512}"
: "${SHAPER_ENV_FILE:=}"  # optional env-file consumed by docker run

run_args=(
  --rm
  --name "${SHAPER_CONTAINER_NAME}"
  --read-only
  --tmpfs /tmp
  --security-opt no-new-privileges:true
  --cpu-shares "${SHAPER_CPU_SHARES}"
  --volume "${SHAPER_CONFIG_PATH}:/etc/oci-cpu-shaper/config.yaml:ro"
)

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
