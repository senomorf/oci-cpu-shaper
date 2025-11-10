package oci

import "context"

// MetricsClient exposes the minimum surface area of the OCI Monitoring API
// required by the adaptive controller.
type MetricsClient interface {
	QueryP95CPU(ctx context.Context, resourceID string) (float64, error)
}
