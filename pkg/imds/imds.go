// Package imds contains clients for the OCI Instance Metadata Service (IMDSv2).
package imds

import "context"

// DefaultEndpoint is the canonical IMDSv2 endpoint for OCI instances.
const DefaultEndpoint = "http://169.254.169.254/opc/v2"

// Client describes the metadata operations needed by the CPU shaper.
type Client interface {
	// Region returns the canonical region for the running instance.
	Region(ctx context.Context) (string, error)
	// CanonicalRegion returns the canonical region name for the running instance.
	CanonicalRegion(ctx context.Context) (string, error)
	// InstanceID returns the OCID of the running instance.
	InstanceID(ctx context.Context) (string, error)
	// CompartmentID returns the compartment OCID for the running instance.
	CompartmentID(ctx context.Context) (string, error)
	// ShapeConfig returns the compute shape attributes for the instance.
	ShapeConfig(ctx context.Context) (ShapeConfig, error)
}

// ShapeConfig contains the compute shape metadata exported by IMDSv2.
type ShapeConfig struct {
	OCPUs                     float64 `json:"ocpus"`
	MemoryInGBs               float64 `json:"memoryInGBs"`
	BaselineOcpuUtilization   string  `json:"baselineOcpuUtilization"`
	BaselineOCPUs             float64 `json:"baselineOcpus"`
	ThreadsPerCore            int     `json:"threadsPerCore"`
	NetworkingBandwidthInGbps float64 `json:"networkingBandwidthInGbps"`
	MaxVnicAttachments        int     `json:"maxVnicAttachments"`
}
