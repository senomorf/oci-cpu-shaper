# OCI CPU Shaper Overview

The OCI CPU Shaper project provides tools for shaping and orchestrating CPU resource usage across Oracle Cloud Infrastructure workloads. The overarching goal is to offer adaptive scheduling, telemetry integration, and policy-driven controls that help teams right-size compute consumption while maintaining service quality.

This overview summarizes the high-level vision and pointers to additional documentation:

- **Architecture and Services** – Describes how command-line tooling, shared packages, and deployment assets fit together. (See forthcoming documents in the `docs/` directory.)
- **Operational Guidance** – Covers build metadata, configuration options, and integration points with OCI metadata services.
- **Contributor Reference** – Explains conventions for extending components in `cmd/`, `pkg/`, and `internal/`.

Additional documents will be added to detail interfaces, deployment flows, and best practices as the project evolves. For local development environment setup and contributor tooling expectations, see [`08-development.md`](./08-development.md).
