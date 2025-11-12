//go:build e2e

package main

import (
	"fmt"
	"os"
	"strings"

	"oci-cpu-shaper/pkg/oci"
	interne2e "oci-cpu-shaper/tests/internal/e2e"
)

//nolint:ireturn // tests rely on MetricsClient interface substitution.
func buildInstancePrincipalMetricsClient(compartmentID, region string) (oci.MetricsClient, error) {
	endpoint := strings.TrimSpace(os.Getenv(interne2e.MonitoringEndpointEnv))
	if endpoint != "" {
		client, err := interne2e.NewMonitoringClient(endpoint)
		if err != nil {
			return nil, fmt.Errorf("build e2e monitoring client: %w", err)
		}

		return client, nil
	}

	client, err := oci.NewInstancePrincipalClient(compartmentID, region)
	if err != nil {
		return nil, fmt.Errorf("new instance principal client: %w", err)
	}

	return &instancePrincipalMetricsClient{client: client}, nil
}
