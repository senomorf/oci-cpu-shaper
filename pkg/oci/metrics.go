// Package oci hosts helpers for interacting with Oracle Cloud Infrastructure APIs.
package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

const (
	monitoringNamespace     = "oci_computeagent"
	metricQueryTemplate     = "CpuUtilization[1m]{resourceId = \"%s\"}.percentile(0.95)"
	metricName              = "CpuUtilization"
	maxOneMinuteWindowHours = 7 * 24
)

var (
	// ErrNoMetricsData indicates that the Monitoring service returned no datapoints for the
	// requested CpuUtilization stream. Callers may fall back to local estimation logic when this
	// sentinel error is returned.
	ErrNoMetricsData = errors.New("oci: cpu utilization metrics unavailable")

	errMissingCompartmentID = errors.New("oci: compartment ID is required")
	errMissingMetricsClient = errors.New("oci: metrics client is required")
	errNilClient            = errors.New("oci: metrics client receiver is nil")
	errMissingInstanceOCID  = errors.New("oci: instance OCID is required")
)

type metricsClient interface {
	SummarizeMetricsData(
		ctx context.Context,
		request monitoring.SummarizeMetricsDataRequest,
		page *string,
	) (monitoring.SummarizeMetricsDataResponse, *string, error)
}

// Client queries tenancy-level Monitoring metrics for the local instance.
type Client struct {
	metrics       metricsClient
	compartmentID string
	now           func() time.Time
}

// NewInstancePrincipalClient constructs a Client backed by the OCI Go SDK using instance principal
// authentication. The compartment OCID identifies the tenancy scope for Monitoring queries.
func NewInstancePrincipalClient(compartmentID string) (*Client, error) {
	if compartmentID == "" {
		return nil, errMissingCompartmentID
	}

	provider, err := auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		return nil, fmt.Errorf("build instance principal provider: %w", err)
	}

	monitoringClient, err := monitoring.NewMonitoringClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create monitoring client: %w", err)
	}

	return newClient(&sdkMonitoringClient{client: &monitoringClient}, compartmentID, time.Now)
}

func newClient(
	metrics metricsClient,
	compartmentID string,
	clock func() time.Time,
) (*Client, error) {
	if metrics == nil {
		return nil, errMissingMetricsClient
	}

	if compartmentID == "" {
		return nil, errMissingCompartmentID
	}

	if clock == nil {
		clock = time.Now
	}

	return &Client{
		metrics:       metrics,
		compartmentID: compartmentID,
		now:           clock,
	}, nil
}

// QueryP95CPU returns the most recent P95 CpuUtilization datapoint for the supplied compute instance.
// When last7d is true the query spans the trailing seven days at one-minute resolution, otherwise a
// 24-hour window is used. The Monitoring API limits one-minute queries to seven days of history, so
// the window is truncated as necessary. ErrNoMetricsData is returned when the API yields no datapoints.
func (c *Client) QueryP95CPU(
	ctx context.Context,
	instanceOCID string,
	last7d bool,
) (float32, error) {
	if c == nil {
		return 0, errNilClient
	}

	if instanceOCID == "" {
		return 0, errMissingInstanceOCID
	}

	start, end := computeWindow(c.now().UTC(), last7d)
	request := buildSummarizeRequest(c.compartmentID, instanceOCID, start, end)

	value, found, err := c.collectLatestDatapoint(ctx, request)
	if err != nil {
		return 0, err
	}

	if !found {
		return 0, ErrNoMetricsData
	}

	return value, nil
}

func computeWindow(now time.Time, last7d bool) (time.Time, time.Time) {
	end := now.Truncate(time.Second)

	start := end.Add(-24 * time.Hour)
	if last7d {
		start = end.Add(-time.Duration(maxOneMinuteWindowHours) * time.Hour)
	}

	maxWindow := time.Duration(maxOneMinuteWindowHours) * time.Hour
	if end.Sub(start) > maxWindow {
		start = end.Add(-maxWindow)
	}

	return start, end
}

func buildSummarizeRequest(
	compartmentID, instanceOCID string,
	start, end time.Time,
) monitoring.SummarizeMetricsDataRequest {
	namespace := monitoringNamespace
	query := fmt.Sprintf(metricQueryTemplate, escapeDimensionValue(instanceOCID))
	startTime := common.SDKTime{Time: start}
	endTime := common.SDKTime{Time: end}

	var details monitoring.SummarizeMetricsDataDetails

	details.Namespace = &namespace
	details.Query = &query
	details.StartTime = &startTime
	details.EndTime = &endTime

	var request monitoring.SummarizeMetricsDataRequest

	request.CompartmentId = &compartmentID
	request.SummarizeMetricsDataDetails = details

	return request
}

func (c *Client) collectLatestDatapoint(
	ctx context.Context,
	request monitoring.SummarizeMetricsDataRequest,
) (float32, bool, error) {
	var (
		pageToken       *string
		latestValue     float32
		latestTimestamp time.Time
	)

	found := false

	for {
		response, nextPage, err := c.metrics.SummarizeMetricsData(ctx, request, pageToken)
		if err != nil {
			return 0, false, fmt.Errorf("summarize metrics: %w", err)
		}

		latestTimestamp, latestValue, found = foldMetricStreams(
			response.Items,
			latestTimestamp,
			latestValue,
			found,
		)

		pageToken = normalizePageToken(nextPage)
		if pageToken == nil {
			break
		}
	}

	if !found {
		return 0, false, nil
	}

	return latestValue, true, nil
}

func foldMetricStreams(
	streams []monitoring.MetricData,
	latestTimestamp time.Time,
	latestValue float32,
	found bool,
) (time.Time, float32, bool) {
	for _, stream := range streams {
		for _, datapoint := range stream.AggregatedDatapoints {
			if datapoint.Value == nil || datapoint.Timestamp == nil {
				continue
			}

			timestamp := datapoint.Timestamp.Time
			if !found || timestamp.After(latestTimestamp) {
				latestTimestamp = timestamp
				latestValue = float32(*datapoint.Value)
				found = true
			}
		}
	}

	return latestTimestamp, latestValue, found
}

func normalizePageToken(token *string) *string {
	if token == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*token)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func escapeDimensionValue(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

// newTestClient exposes constructor hooks for unit tests.
func newTestClient(
	metrics metricsClient,
	compartmentID string,
	clock func() time.Time,
) (*Client, error) {
	return newClient(metrics, compartmentID, clock)
}

type sdkMonitoringClient struct {
	client *monitoring.MonitoringClient
}

func (s *sdkMonitoringClient) SummarizeMetricsData(
	ctx context.Context,
	request monitoring.SummarizeMetricsDataRequest,
	page *string,
) (monitoring.SummarizeMetricsDataResponse, *string, error) {
	httpRequest, err := request.HTTPRequest(
		http.MethodPost,
		"/metrics/actions/summarizeMetricsData",
		nil,
		nil,
	)
	if err != nil {
		return monitoring.SummarizeMetricsDataResponse{}, nil, fmt.Errorf(
			"build summarize request: %w",
			err,
		)
	}

	if trimmed := normalizePageToken(page); trimmed != nil {
		query := httpRequest.URL.Query()
		query.Set("page", *trimmed)
		httpRequest.URL.RawQuery = query.Encode()
	}

	httpResponse, err := s.client.Call(ctx, &httpRequest)

	if httpResponse != nil {
		defer func() {
			common.CloseBodyIfValid(httpResponse)
		}()
	}

	var response monitoring.SummarizeMetricsDataResponse

	response.RawResponse = httpResponse

	if err != nil {
		apiReferenceLink := "https://docs.oracle.com/iaas/api/#/en/monitoring/20180401/MetricData/SummarizeMetricsData"
		wrapped := common.PostProcessServiceError(
			err,
			"Monitoring",
			"SummarizeMetricsData",
			apiReferenceLink,
		)

		return response, nil, fmt.Errorf("execute summarize metrics request: %w", wrapped)
	}

	err = common.UnmarshalResponse(httpResponse, &response)
	if err != nil {
		return response, nil, fmt.Errorf("decode summarize metrics response: %w", err)
	}

	headerValue := httpResponse.Header.Get("Opc-Next-Page")

	return response, normalizePageToken(&headerValue), nil
}
