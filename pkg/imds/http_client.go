package imds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHTTPClientTimeout = 2 * time.Second
	defaultMaxAttempts       = 3
	defaultBackoff           = 200 * time.Millisecond
	metadataAuthorization    = "Bearer Oracle"
)

var (
	errRetryableStatus  = errors.New("imds: retryable status code")
	errUnexpectedStatus = errors.New("imds: unexpected status code")
	errExhaustedRetries = errors.New("imds: exhausted retry budget")
	errRequestFailed    = errors.New("imds: request execution failed")
)

type clientConfig struct {
	baseURL    string
	maxAttempt int
	backoff    time.Duration
}

// Option mutates the HTTP client configuration during construction.
type Option func(*clientConfig)

// WithBaseURL overrides the metadata service base URL used for requests.
func WithBaseURL(baseURL string) Option {
	return func(cfg *clientConfig) {
		trimmed := strings.TrimSpace(baseURL)
		if trimmed == "" {
			return
		}

		cfg.baseURL = trimmed
	}
}

// WithMaxAttempts overrides the retry budget for metadata requests.
func WithMaxAttempts(attempts int) Option {
	return func(cfg *clientConfig) {
		if attempts > 0 {
			cfg.maxAttempt = attempts
		}
	}
}

// WithBackoff overrides the delay between retry attempts.
func WithBackoff(delay time.Duration) Option {
	return func(cfg *clientConfig) {
		if delay > 0 {
			cfg.backoff = delay
		}
	}
}

// NewClient constructs an HTTP-backed IMDS client. A nil httpClient uses a
// private instance with a conservative timeout suitable for link-local access.
//
//nolint:ireturn // callers depend on the Client abstraction for substitution.
func NewClient(httpClient *http.Client, opts ...Option) Client {
	cfg := clientConfig{
		baseURL:    DefaultEndpoint,
		maxAttempt: defaultMaxAttempts,
		backoff:    defaultBackoff,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		opt(&cfg)
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       defaultHTTPClientTimeout,
			Transport:     http.DefaultTransport,
			CheckRedirect: http.DefaultClient.CheckRedirect,
			Jar:           http.DefaultClient.Jar,
		}
	}

	return &HTTPClient{
		http:       httpClient,
		baseURL:    strings.TrimRight(cfg.baseURL, "/"),
		maxAttempt: cfg.maxAttempt,
		backoff:    cfg.backoff,
	}
}

// HTTPClient issues metadata requests against the OCI IMDSv2 service.
type HTTPClient struct {
	http       *http.Client
	baseURL    string
	maxAttempt int
	backoff    time.Duration
}

// Region returns the canonical region for the running instance.
func (c *HTTPClient) Region(ctx context.Context) (string, error) {
	body, err := c.getText(ctx, "region")
	if err != nil {
		return "", err
	}

	return body, nil
}

// CanonicalRegion returns the canonical region name for the running instance.
func (c *HTTPClient) CanonicalRegion(ctx context.Context) (string, error) {
	var info regionInfo

	err := c.getJSON(ctx, "regionInfo", &info)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(info.CanonicalRegionName), nil
}

// InstanceID returns the OCID for the running instance.
func (c *HTTPClient) InstanceID(ctx context.Context) (string, error) {
	body, err := c.getText(ctx, "id")
	if err != nil {
		return "", err
	}

	return body, nil
}

// CompartmentID returns the compartment OCID for the running instance.
func (c *HTTPClient) CompartmentID(ctx context.Context) (string, error) {
	body, err := c.getText(ctx, "compartmentId")
	if err != nil {
		return "", err
	}

	return body, nil
}

// ShapeConfig returns the compute shape metadata for the running instance.
func (c *HTTPClient) ShapeConfig(ctx context.Context) (ShapeConfig, error) {
	var cfg ShapeConfig

	err := c.getJSON(ctx, "shape-config", &cfg)
	if err != nil {
		return ShapeConfig{}, err
	}

	return cfg, nil
}

func (c *HTTPClient) getText(ctx context.Context, resource string) (string, error) {
	payload, err := c.fetch(ctx, resource)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(payload)), nil
}

func (c *HTTPClient) getJSON(ctx context.Context, resource string, out any) error {
	payload, err := c.fetch(ctx, resource)
	if err != nil {
		return err
	}

	decodeErr := json.Unmarshal(payload, out)
	if decodeErr != nil {
		return fmt.Errorf("decode %s response: %w", resource, decodeErr)
	}

	return nil
}

func (c *HTTPClient) fetch(ctx context.Context, resource string) ([]byte, error) {
	var lastErr error

	for attempt := 1; attempt <= c.maxAttempt; attempt++ {
		payload, retry, err := c.tryFetch(ctx, resource)
		if err == nil {
			return payload, nil
		}

		if !retry {
			return nil, err
		}

		lastErr = err

		if attempt == c.maxAttempt {
			break
		}

		waitErr := c.wait(ctx)
		if waitErr != nil {
			return nil, fmt.Errorf("retry wait for %s: %w", resource, waitErr)
		}
	}

	if lastErr == nil {
		return nil, fmt.Errorf("%w: %s", errExhaustedRetries, resource)
	}

	return nil, fmt.Errorf("%w: %w", errExhaustedRetries, lastErr)
}

func (c *HTTPClient) wait(ctx context.Context) error {
	timer := time.NewTimer(c.backoff)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("context done while waiting to retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (c *HTTPClient) tryFetch(ctx context.Context, resource string) ([]byte, bool, error) {
	req, err := metadataRequest(ctx, http.MethodGet, c.resourceURL(resource))
	if err != nil {
		return nil, false, fmt.Errorf("build request for %s: %w", resource, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, false, fmt.Errorf("%w: %s: %w", errRequestFailed, resource, ctxErr)
		}

		return nil, true, fmt.Errorf("%w: %s: %w", errRequestFailed, resource, err)
	}

	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()

	if readErr != nil {
		if closeErr != nil {
			wrap := fmt.Errorf("close response body: %w", closeErr)
			readErr = errors.Join(readErr, wrap)
		}

		return nil, false, fmt.Errorf("read %s response: %w", resource, readErr)
	}

	if closeErr != nil {
		return nil, false, fmt.Errorf("close %s response body: %w", resource, closeErr)
	}

	if resp.StatusCode == http.StatusOK {
		return body, false, nil
	}

	if !isRetryable(resp.StatusCode) {
		trimmed := strings.TrimSpace(string(body))

		return nil, false, fmt.Errorf(
			"%w: %s (status %d, body %s)",
			errUnexpectedStatus,
			resource,
			resp.StatusCode,
			trimmed,
		)
	}

	return nil, true, fmt.Errorf(
		"%w: %s (status %d)",
		errRetryableStatus,
		resource,
		resp.StatusCode,
	)
}

func (c *HTTPClient) resourceURL(resource string) string {
	trimmed := strings.TrimPrefix(resource, "/")
	base := strings.TrimRight(c.baseURL, "/")

	return fmt.Sprintf("%s/instance/%s", base, trimmed)
}

func isRetryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return status >= 500 && status != http.StatusNotImplemented
	}
}

func metadataRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build metadata request: %w", err)
	}

	req.Header.Set("Authorization", metadataAuthorization)

	return req, nil
}

type regionInfo struct {
	CanonicalRegionName string `json:"canonicalRegionName"`
}
