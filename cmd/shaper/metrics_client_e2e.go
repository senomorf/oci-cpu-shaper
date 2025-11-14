//go:build e2e

package main

import (
	"fmt"
	"os"
	"strings"

	"oci-cpu-shaper/internal/e2eclient"
	"oci-cpu-shaper/pkg/oci"
)

//nolint:ireturn // tests rely on MetricsClient interface substitution.
func buildInstancePrincipalMetricsClient(compartmentID, region string) (oci.MetricsClient, error) {
	endpoint := strings.TrimSpace(os.Getenv(e2eclient.MonitoringEndpointEnv))
	if endpoint != "" {
		client, err := e2eclient.NewMonitoringClient(endpoint)
		if err != nil {
			return nil, fmt.Errorf("build e2e monitoring client: %w", err)
		}

		return client, nil
	}

	client, err := newInstancePrincipalClient(compartmentID, region)
	if err != nil {
		return nil, fmt.Errorf("new instance principal client: %w", err)
	}

	return &instancePrincipalMetricsClient{client: client}, nil
}
