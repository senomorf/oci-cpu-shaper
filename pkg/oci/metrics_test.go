package oci //nolint:testpackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

var (
	errNoMockResponse = errors.New("http mock: no response configured")
	errForcedFailure  = errors.New("http mock: forced failure")
)

type httpVerifyingClient struct {
	t          *testing.T
	endpoint   string
	httpClient *http.Client

	mu         sync.Mutex
	requests   []monitoring.SummarizeMetricsDataRequest
	responses  []monitoring.SummarizeMetricsDataResponse
	pages      []string
	nextTokens []string
	err        error
}

func (c *httpVerifyingClient) SummarizeMetricsData(
	ctx context.Context,
	request monitoring.SummarizeMetricsDataRequest,
	page *string,
) (monitoring.SummarizeMetricsDataResponse, *string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return monitoring.SummarizeMetricsDataResponse{}, nil, c.err
	}

	payload, token := buildRequestPayload(request, page)

	err := c.postPayload(ctx, payload)
	if err != nil {
		return monitoring.SummarizeMetricsDataResponse{}, nil, err
	}

	c.requests = append(c.requests, request)
	c.pages = append(c.pages, token)

	if len(c.responses) == 0 {
		return monitoring.SummarizeMetricsDataResponse{}, nil, errNoMockResponse
	}

	response := c.responses[0]
	c.responses = c.responses[1:]

	if len(c.nextTokens) == 0 {
		return response, nil, nil
	}

	next := strings.TrimSpace(c.nextTokens[0])
	c.nextTokens = c.nextTokens[1:]

	if next == "" {
		return response, nil, nil
	}

	return response, &next, nil
}

func buildRequestPayload(
	request monitoring.SummarizeMetricsDataRequest,
	page *string,
) (map[string]any, string) {
	payload := map[string]any{}
	details := request.SummarizeMetricsDataDetails

	if details.Query != nil {
		payload["query"] = *details.Query
	}

	if details.StartTime != nil {
		payload["startTime"] = details.StartTime.Format(time.RFC3339)
	}

	if details.EndTime != nil {
		payload["endTime"] = details.EndTime.Format(time.RFC3339)
	}

	trimmed := ""
	if page != nil {
		trimmed = strings.TrimSpace(*page)
		if trimmed != "" {
			payload["page"] = trimmed
		}
	}

	return payload, trimmed
}

func (c *httpVerifyingClient) postPayload(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("issue mock request: %w", err)
	}

	defer func() {
		_ = httpResponse.Body.Close()
	}()

	return nil
}

func TestQueryP95CPUFetchesLatestDatapoint(t *testing.T) {
	t.Parallel()

	instanceID := "ocid1.instance.oc1.phx.exampleuniqueID"
	compartmentID := "ocid1.compartment.oc1..exampleuniqueID"
	now := time.Date(2025, time.January, 2, 15, 4, 5, 0, time.UTC)

	expectedQuery := "CpuUtilization[1m]{resourceId = \"" + instanceID + "\"}.percentile(0.95)"

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			t.Helper()

			defer func() {
				_ = req.Body.Close()
			}()

			var payload map[string]string

			err := json.NewDecoder(req.Body).Decode(&payload)
			requireNoError(t, err, "decode payload")

			requireEqual(t, payload["query"], expectedQuery, "unexpected query")

			if payload["startTime"] == "" || payload["endTime"] == "" {
				t.Fatalf("expected start and end time in payload: %#v", payload)
			}

			writer.WriteHeader(http.StatusOK)
		}),
	)
	t.Cleanup(server.Close)

	responses := []monitoring.SummarizeMetricsDataResponse{
		metricResponse(metricData(instanceID, compartmentID, now.Add(-10*time.Minute), 12.5)),
		metricResponse(metricData(instanceID, compartmentID, now.Add(-5*time.Minute), 18.75)),
	}

	verifying := newHTTPVerifyingClient(t, server, responses, []string{"next"})

	client, err := newTestClient(verifying, compartmentID, func() time.Time { return now })
	requireNoError(t, err, "create client")

	value, err := client.QueryP95CPU(context.Background(), instanceID, true)
	requireNoError(t, err, "QueryP95CPU")

	requireEqual(t, value, float32(18.75), "unexpected value")

	verifying.mu.Lock()
	defer verifying.mu.Unlock()

	requireEqual(t, len(verifying.requests), 2, "request count")
	assertRequestWindow(t, verifying.requests[0], now.Add(-7*24*time.Hour), now)

	requireEqual(t, len(verifying.pages), 2, "page count")
	requireEqual(t, verifying.pages[0], "", "first page token")
	requireEqual(t, verifying.pages[1], "next", "second page token")
}

func TestQueryP95CPUHandlesMissingData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	verifying := newHTTPVerifyingClient(
		t,
		server,
		[]monitoring.SummarizeMetricsDataResponse{metricResponse()},
		nil,
	)

	client, err := newTestClient(
		verifying,
		"ocid1.compartment.oc1..exampleuniqueID",
		func() time.Time {
			return time.Now().UTC()
		},
	)
	requireNoError(t, err, "create client")

	_, err = client.QueryP95CPU(context.Background(), "ocid1.instance.oc1.phx.empty", false)
	if !errors.Is(err, ErrNoMetricsData) {
		t.Fatalf("expected ErrNoMetricsData, got %v", err)
	}
}

func TestQueryP95CPUPropagatesErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	verifying := newHTTPVerifyingClient(t, server, nil, nil)
	verifying.err = errForcedFailure

	client, err := newTestClient(verifying, "ocid1.compartment.oc1..exampleuniqueID", time.Now)
	requireNoError(t, err, "create client")

	_, err = client.QueryP95CPU(context.Background(), "ocid1.instance.oc1.phx.failure", false)
	if err == nil || !strings.Contains(err.Error(), "summarize metrics") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func newHTTPVerifyingClient(
	t *testing.T,
	server *httptest.Server,
	responses []monitoring.SummarizeMetricsDataResponse,
	nextTokens []string,
) *httpVerifyingClient {
	t.Helper()

	return &httpVerifyingClient{
		t:          t,
		endpoint:   server.URL,
		httpClient: server.Client(),
		mu:         sync.Mutex{},
		requests:   nil,
		responses:  append([]monitoring.SummarizeMetricsDataResponse{}, responses...),
		pages:      nil,
		nextTokens: append([]string(nil), nextTokens...),
		err:        nil,
	}
}

func metricData(
	instanceID, compartmentID string,
	timestamp time.Time,
	value float64,
) monitoring.MetricData {
	var datapoint monitoring.AggregatedDatapoint

	datapoint.Timestamp = &common.SDKTime{Time: timestamp}
	datapoint.Value = common.Float64(value)

	var data monitoring.MetricData

	data.Namespace = common.String(monitoringNamespace)
	data.CompartmentId = common.String(compartmentID)
	data.Name = common.String(metricName)
	data.Dimensions = map[string]string{"resourceId": instanceID}
	data.AggregatedDatapoints = []monitoring.AggregatedDatapoint{datapoint}
	data.ResourceGroup = nil
	data.Metadata = nil
	data.Resolution = nil

	return data
}

func metricResponse(items ...monitoring.MetricData) monitoring.SummarizeMetricsDataResponse {
	var response monitoring.SummarizeMetricsDataResponse

	response.RawResponse = nil
	response.Items = append(response.Items, items...)
	response.OpcRequestId = nil

	return response
}

func requireNoError(t *testing.T, err error, message string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T, message string) {
	t.Helper()

	if got != want {
		t.Fatalf("%s: got %v want %v", message, got, want)
	}
}

func assertRequestWindow(
	t *testing.T,
	request monitoring.SummarizeMetricsDataRequest,
	start, end time.Time,
) {
	t.Helper()

	details := request.SummarizeMetricsDataDetails
	if details.StartTime == nil || details.EndTime == nil {
		t.Fatalf("request missing timestamps: %#v", details)
	}

	requireEqual(t, details.StartTime.Time, start, "unexpected start time")
	requireEqual(t, details.EndTime.Time, end, "unexpected end time")
}
