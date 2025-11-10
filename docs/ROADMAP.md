# Roadmap

## 14.1 CI automation
- Run `golangci-lint` with the repository defaults on every pull request to catch regressions early (§14).
- Execute `go test ./... -race` for all targets so concurrency issues surface during review (§14).
- Follow the [§11 Coverage Workflow](08-development.md#11-coverage-workflow) to keep statement coverage at or above the CI floor during feature work (§14).
- Block merges on CI completion to keep `main` green.
- Cache Go modules across CI jobs so linting and testing complete quickly while using the latest Go toolchain (§14).
- Build the container image with Docker Buildx on every pull request, reusing cached layers,
  and run a dry-run smoke test via the packaged CLI before merging (§14).
- Generate an SPDX SBOM for each pull-request build and gate merges on vulnerability scanning
  that fails when critical issues appear (§14).

## 14.2 Release pipeline
- Trigger releases from git tags prefixed with `v` to map cleanly to container tags (§14).
- Build multi-architecture images (`linux/amd64`, `linux/arm64`) via `docker buildx` and push them to GHCR for distribution (§4).
- Generate an SPDX SBOM with Syft for each release image and store it alongside build artifacts (§14).
- Reuse Buildx cache layers via GitHub Actions cache to accelerate repeat release builds (§14).
- Pin SBOM generation to the latest Anchore Syft GitHub Action outputting SPDX JSON artifacts (§14).

### Release checklist
1. Cut a `v*` tag from a green `main`.
2. Confirm the Release workflow pushes both versioned and `latest` images to GHCR.
3. Verify the generated SBOM artifact and attach it to any manual release notes.
4. Announce availability and update downstream deployment references if necessary.
