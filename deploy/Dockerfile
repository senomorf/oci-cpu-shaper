# syntax=docker/dockerfile:1.6

ARG GO_VERSION=1.24.3
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION="dev"
ARG GIT_COMMIT="unknown"
ARG BUILD_DATE="unknown"

ENV CGO_ENABLED=0

RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH:-amd64} GOARM=${TARGETVARIANT#v} \
    go build \
      -trimpath \
      -ldflags="-s -w -X oci-cpu-shaper/internal/buildinfo.Version=${VERSION} -X oci-cpu-shaper/internal/buildinfo.GitCommit=${GIT_COMMIT} -X oci-cpu-shaper/internal/buildinfo.BuildDate=${BUILD_DATE}" \
      -o /out/oci-cpu-shaper \
      ./cmd/shaper

FROM gcr.io/distroless/static:nonroot AS nonroot

LABEL org.opencontainers.image.title="oci-cpu-shaper" \
      org.opencontainers.image.description="OCI CPU shaper (distroless nonroot)" \
      org.opencontainers.image.source="https://github.com/oci-cpu-shaper/oci-cpu-shaper"

COPY --from=builder /out/oci-cpu-shaper /usr/local/bin/oci-cpu-shaper
COPY deploy/config/offline-smoke.yaml /etc/oci-cpu-shaper/config.yaml

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/oci-cpu-shaper"]

FROM gcr.io/distroless/static:latest AS rootful

LABEL org.opencontainers.image.title="oci-cpu-shaper" \
      org.opencontainers.image.description="OCI CPU shaper (distroless rootful)" \
      org.opencontainers.image.source="https://github.com/oci-cpu-shaper/oci-cpu-shaper"

COPY --from=builder /out/oci-cpu-shaper /usr/local/bin/oci-cpu-shaper
COPY deploy/config/offline-smoke.yaml /etc/oci-cpu-shaper/config.yaml

USER 0:0

ENTRYPOINT ["/usr/local/bin/oci-cpu-shaper"]
