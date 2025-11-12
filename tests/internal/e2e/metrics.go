package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const metricsPollInterval = 100 * time.Millisecond

// WaitForMetrics polls the provided URL until a 200 response with a non-empty body is observed or the context expires.
func WaitForMetrics(ctx context.Context, url string) ([]byte, error) {
	client := http.Client{ //nolint:exhaustruct // only timeout configured by context
		Timeout: time.Second,
	}

	ticker := time.NewTicker(metricsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for metrics: %w", ctx.Err())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			if err != nil {
				return nil, fmt.Errorf("wait for metrics: build request: %w", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				continue
			}

			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				_ = resp.Body.Close()

				return nil, fmt.Errorf("wait for metrics: read body: %w", readErr)
			}

			closeErr := resp.Body.Close()
			if closeErr != nil {
				return nil, fmt.Errorf("wait for metrics: close body: %w", closeErr)
			}

			if resp.StatusCode != http.StatusOK {
				continue
			}

			if len(body) == 0 {
				continue
			}

			return body, nil
		}
	}
}
