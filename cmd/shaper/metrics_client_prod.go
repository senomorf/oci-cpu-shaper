//go:build !e2e

package main

import (
	"fmt"

	"oci-cpu-shaper/pkg/oci"
)

//nolint:ireturn // helper returns MetricsClient interface for controller wiring.
func buildInstancePrincipalMetricsClient(compartmentID, region string) (oci.MetricsClient, error) {
	client, err := newInstancePrincipalClient(compartmentID, region)
	if err != nil {
		return nil, fmt.Errorf("new instance principal client: %w", err)
	}

	return &instancePrincipalMetricsClient{client: client}, nil
}
