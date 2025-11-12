package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"oci-cpu-shaper/pkg/oci"
)

const (
	// MonitoringEndpointEnv configures the HTTP endpoint used by the e2e metrics client.
	MonitoringEndpointEnv = "OCI_CPU_SHAPER_E2E_MONITORING_ENDPOINT"

	defaultHTTPTimeout = 2 * time.Second
	responseBodyLimit  = 512
)

var (
	errMonitoringEndpointRequired   = errors.New("monitoring client: endpoint is required")
	errMonitoringHTTPNotInitialised = errors.New("monitoring client: http client not initialised")
	errMonitoringUnexpectedStatus   = errors.New("monitoring client: unexpected status")
	errMonitoringResponseBody       = errors.New("monitoring client: response body")
)

type monitoringPayload struct {
	Value float64 `json:"value"`
}

// NewMonitoringClient constructs an oci.MetricsClient backed by HTTP endpoints exposed
// by the e2e monitoring server helpers.
//
//nolint:ireturn // tests rely on the MetricsClient interface for controller wiring.
func NewMonitoringClient(endpoint string) (oci.MetricsClient, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return nil, errMonitoringEndpointRequired
	}

	return &monitoringClient{
		endpoint: trimmed,
		http: &http.Client{ //nolint:exhaustruct // only timeout customised for tests
			Timeout: defaultHTTPTimeout,
		},
	}, nil
}

type monitoringClient struct {
	endpoint string
	http     *http.Client
}

func (c *monitoringClient) QueryP95CPU(ctx context.Context, resourceID string) (float64, error) {
	if c == nil || c.http == nil {
		return 0, errMonitoringHTTPNotInitialised
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("monitoring client: build request: %w", err)
	}

	query := url.Values{}
	query.Set("resource", resourceID)
	req.URL.RawQuery = query.Encode()

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("monitoring client: execute request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNoContent {
		return 0, oci.ErrNoMetricsData
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, responseBodyLimit))
		if len(body) == 0 {
			return 0, fmt.Errorf("%w: %d", errMonitoringUnexpectedStatus, resp.StatusCode)
		}

		return 0, fmt.Errorf("%w: %s", errMonitoringResponseBody, strings.TrimSpace(string(body)))
	}

	var payload monitoringPayload

	decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
	if decodeErr != nil {
		return 0, fmt.Errorf("monitoring client: decode payload: %w", decodeErr)
	}

	return payload.Value, nil
}
