package oci //nolint:testpackage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	providerOverrides     []providerOverride   //nolint:gochecknoglobals
	providerOverrideSeq   uint64               //nolint:gochecknoglobals
	monitoringOverrides   []monitoringOverride //nolint:gochecknoglobals
	monitoringOverrideSeq uint64               //nolint:gochecknoglobals
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

func TestComputeWindowRespectsLookbackLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.July, 1, 15, 30, 45, 123456789, time.UTC)

	t.Run("24h-window", func(t *testing.T) {
		t.Parallel()

		t.Helper()

		start, end := computeWindow(now, false)

		requireEqual(t, end, now.Truncate(time.Second), "end timestamp truncated")
		requireEqual(t, start, end.Add(-24*time.Hour), "24h lookback")
	})

	t.Run("7d-window-truncated", func(t *testing.T) {
		t.Parallel()

		t.Helper()

		start, end := computeWindow(now, true)

		expectedWindow := time.Duration(maxOneMinuteWindowHours) * time.Hour

		requireEqual(t, end, now.Truncate(time.Second), "end timestamp truncated")
		requireEqual(t, start, end.Add(-expectedWindow), "seven day lookback")
	})
}

func TestBuildSummarizeRequestEscapesInstanceOCID(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, time.June, 30, 14, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	compartmentID := "ocid1.compartment.oc1..exampleuniqueID"
	instanceID := "ocid1.instance.oc1..example\"uniqueID"

	request := buildSummarizeRequest(compartmentID, instanceID, start, end)

	if request.CompartmentId == nil {
		t.Fatalf("request missing compartment ID: %#v", request)
	}

	requireEqual(t, *request.CompartmentId, compartmentID, "compartment ID")

	details := request.SummarizeMetricsDataDetails

	if details.Query == nil {
		t.Fatalf("request missing query: %#v", details)
	}

	expectedQuery := fmt.Sprintf(metricQueryTemplate, escapeDimensionValue(instanceID))
	requireEqual(t, *details.Query, expectedQuery, "escaped query")

	if details.StartTime == nil || details.EndTime == nil {
		t.Fatalf("request missing timestamps: %#v", details)
	}

	requireEqual(t, details.StartTime.Time, start, "start time")
	requireEqual(t, details.EndTime.Time, end, "end time")
}

func TestCollectLatestDatapointAggregatesAcrossPages(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.June, 30, 16, 0, 0, 0, time.UTC)

	responses := []monitoring.SummarizeMetricsDataResponse{
		metricResponse(
			metricData("ocid.instance", "ocid.compartment", now.Add(-90*time.Minute), 10.0),
			metricData("ocid.instance", "ocid.compartment", now.Add(-45*time.Minute), 12.5),
			metricDataWithNilFields(),
		),
		metricResponse(
			metricData("ocid.instance", "ocid.compartment", now.Add(-15*time.Minute), 18.75),
		),
	}

	tokens := []*string{
		stringPointer(" next-page "),
		stringPointer("   "),
	}

	stub := newStubMetricsClient(responses, tokens, nil)

	client, err := newTestClient(stub, "ocid.compartment", func() time.Time { return now })
	requireNoError(t, err, "create client")

	request := buildSummarizeRequest(
		"ocid.compartment",
		"ocid.instance",
		now.Add(-2*time.Hour),
		now,
	)

	value, found, err := client.collectLatestDatapoint(context.Background(), request)
	requireNoError(t, err, "collect datapoint")

	if !found {
		t.Fatalf("expected to find datapoint")
	}

	requireEqual(t, value, float32(18.75), "latest datapoint")

	if stub.calls != 2 {
		t.Fatalf("expected 2 API calls, got %d", stub.calls)
	}
}

func TestCollectLatestDatapointHandlesEmptyResponses(t *testing.T) {
	t.Parallel()

	stub := newStubMetricsClient(
		[]monitoring.SummarizeMetricsDataResponse{metricResponse()},
		nil,
		nil,
	)

	client, err := newTestClient(stub, "ocid.compartment", time.Now)
	requireNoError(t, err, "create client")

	request := buildSummarizeRequest(
		"ocid.compartment",
		"ocid.instance",
		time.Now().Add(-time.Hour),
		time.Now(),
	)

	_, found, err := client.collectLatestDatapoint(context.Background(), request)
	requireNoError(t, err, "collect datapoint")

	if found {
		t.Fatalf("expected no datapoint to be found")
	}
}

func TestCollectLatestDatapointPropagatesErrors(t *testing.T) {
	t.Parallel()

	stub := newStubMetricsClient(nil, nil, errForcedFailure)

	client, err := newTestClient(stub, "ocid.compartment", time.Now)
	requireNoError(t, err, "create client")

	request := buildSummarizeRequest(
		"ocid.compartment",
		"ocid.instance",
		time.Now().Add(-time.Hour),
		time.Now(),
	)

	_, _, err = client.collectLatestDatapoint(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "summarize metrics") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestNormalizePageToken(t *testing.T) {
	t.Parallel()

	if token := normalizePageToken(nil); token != nil {
		t.Fatalf("expected nil for nil token, got %#v", token)
	}

	whitespace := "  \t  "
	if token := normalizePageToken(&whitespace); token != nil {
		t.Fatalf("expected nil for whitespace token, got %#v", token)
	}

	raw := " next "

	token := normalizePageToken(&raw)
	if token == nil || *token != "next" {
		t.Fatalf("expected trimmed token 'next', got %#v", token)
	}
}

func TestEscapeDimensionValue(t *testing.T) {
	t.Parallel()

	input := `ocid1.instance.oc1..example"uniqueID`
	expected := `ocid1.instance.oc1..example\"uniqueID`

	requireEqual(t, escapeDimensionValue(input), expected, "escaped value")
}

func TestNewClientValidatesParameters(t *testing.T) {
	t.Parallel()

	_, err := newClient(nil, "ocid.compartment", time.Now)
	if !errors.Is(err, errMissingMetricsClient) {
		t.Fatalf("expected errMissingMetricsClient, got %v", err)
	}

	_, err = newClient(newStubMetricsClient(nil, nil, nil), "", time.Now)
	if !errors.Is(err, errMissingCompartmentID) {
		t.Fatalf("expected errMissingCompartmentID, got %v", err)
	}

	client, err := newClient(newStubMetricsClient(nil, nil, nil), "ocid.compartment", nil)
	requireNoError(t, err, "create client with default clock")

	if client == nil || client.now == nil {
		t.Fatalf("expected client with default clock, got %#v", client)
	}
}

func TestNewInstancePrincipalClientPropagatesProviderError(t *testing.T) {
	t.Parallel()

	overrideInstancePrincipalProvider(t, func() (common.ConfigurationProvider, error) {
		return nil, errForcedFailure
	})

	_, err := NewInstancePrincipalClient("ocid1.compartment.oc1..exampleuniqueID")
	if err == nil || !strings.Contains(err.Error(), "build instance principal provider") {
		t.Fatalf("expected wrapped provider error, got %v", err)
	}
}

func TestNewInstancePrincipalClientPropagatesClientError(t *testing.T) {
	t.Parallel()

	provider := stubConfigurationProvider(t)

	overrideInstancePrincipalProvider(t, func() (common.ConfigurationProvider, error) {
		return provider, nil
	})

	overrideNewMonitoringClient(
		t,
		func(common.ConfigurationProvider) (monitoring.MonitoringClient, error) {
			var client monitoring.MonitoringClient

			return client, errForcedFailure
		},
	)

	_, err := NewInstancePrincipalClient("ocid1.compartment.oc1..exampleuniqueID")
	if err == nil || !strings.Contains(err.Error(), "create monitoring client") {
		t.Fatalf("expected monitoring client error, got %v", err)
	}
}

func TestNewInstancePrincipalClientSuccess(t *testing.T) {
	t.Parallel()

	provider := stubConfigurationProvider(t)

	overrideInstancePrincipalProvider(t, func() (common.ConfigurationProvider, error) {
		return provider, nil
	})

	overrideNewMonitoringClient(
		t,
		func(common.ConfigurationProvider) (monitoring.MonitoringClient, error) {
			var client monitoring.MonitoringClient

			return client, nil
		},
	)

	client, err := NewInstancePrincipalClient("ocid1.compartment.oc1..exampleuniqueID")
	requireNoError(t, err, "construct instance principal client")

	if client == nil {
		t.Fatalf("expected client instance")
	}

	requireEqual(
		t,
		client.compartmentID,
		"ocid1.compartment.oc1..exampleuniqueID",
		"compartment ID",
	)

	sdkClient, ok := client.metrics.(*sdkMonitoringClient)
	if !ok || sdkClient == nil || sdkClient.client == nil {
		t.Fatalf("expected sdkMonitoringClient, got %#v", client.metrics)
	}
}

func TestSDKMonitoringClientSummarizeMetricsDataSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.June, 30, 16, 0, 0, 0, time.UTC)

	body := fmt.Sprintf(
		`[`+
			`{"namespace":"oci_computeagent","compartmentId":"%s","name":"CpuUtilization",`+
			`"dimensions":{"resourceId":"%s"},`+
			`"aggregatedDatapoints":[{"timestamp":"%s","value":42.5}]}`+
			`]`,
		"ocid.compartment",
		"ocid.instance",
		now.Format(time.RFC3339),
	)

	headers := http.Header{
		"Content-Type":  []string{"application/json"},
		"Opc-Next-Page": []string{"  next-page  "},
	}

	caller := newStubAPICaller(newJSONResponse(body, headers), nil) //nolint:bodyclose
	client := &sdkMonitoringClient{client: caller}

	request := buildSummarizeRequest("ocid.compartment", "ocid.instance", now.Add(-time.Hour), now)
	summary, next, err := client.SummarizeMetricsData(
		context.Background(),
		request,
		stringPointer("  token  "),
	)
	requireNoError(t, err, "summarize metrics")

	assertNextPageToken(t, next, "next-page")
	assertSummarizeRequest(t, caller.lastRequest, "token")
	assertSummaryDatapoint(t, summary, now, 42.5)
}

func TestSDKMonitoringClientSummarizeMetricsDataWrapsCallErrors(t *testing.T) {
	t.Parallel()

	caller := newStubAPICaller(nil, errForcedFailure)

	client := &sdkMonitoringClient{client: caller}

	request := buildSummarizeRequest(
		"ocid.compartment",
		"ocid.instance",
		time.Now().Add(-time.Minute),
		time.Now(),
	)

	_, _, err := client.SummarizeMetricsData(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "execute summarize metrics request") {
		t.Fatalf("expected wrapped call error, got %v", err)
	}
}

func TestSDKMonitoringClientSummarizeMetricsDataHandlesDecodeErrors(t *testing.T) {
	t.Parallel()

	response := new(http.Response)
	response.StatusCode = http.StatusOK
	response.Header = http.Header{"Content-Type": []string{"application/json"}}
	response.Body = io.NopCloser(strings.NewReader("not-json"))

	caller := newStubAPICaller(response, nil)

	client := &sdkMonitoringClient{client: caller}

	request := buildSummarizeRequest(
		"ocid.compartment",
		"ocid.instance",
		time.Now().Add(-time.Minute),
		time.Now(),
	)

	_, _, err := client.SummarizeMetricsData(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "decode summarize metrics response") {
		t.Fatalf("expected decode error, got %v", err)
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

func metricDataWithNilFields() monitoring.MetricData {
	var datapoint monitoring.AggregatedDatapoint

	datapoint.Timestamp = nil
	datapoint.Value = nil

	var data monitoring.MetricData

	data.Namespace = common.String(monitoringNamespace)
	data.AggregatedDatapoints = []monitoring.AggregatedDatapoint{datapoint}

	return data
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

func newStubMetricsClient(
	responses []monitoring.SummarizeMetricsDataResponse,
	tokens []*string,
	err error,
) *stubMetricsClient {
	copiedResponses := append([]monitoring.SummarizeMetricsDataResponse(nil), responses...)
	copiedTokens := append([]*string(nil), tokens...)

	return &stubMetricsClient{
		responses: copiedResponses,
		tokens:    copiedTokens,
		err:       err,
		calls:     0,
	}
}

type stubMetricsClient struct {
	responses []monitoring.SummarizeMetricsDataResponse
	tokens    []*string
	err       error

	calls int
}

func (s *stubMetricsClient) SummarizeMetricsData(
	_ context.Context,
	_ monitoring.SummarizeMetricsDataRequest,
	_ *string,
) (monitoring.SummarizeMetricsDataResponse, *string, error) {
	s.calls++

	if s.err != nil {
		return monitoring.SummarizeMetricsDataResponse{}, nil, s.err
	}

	if len(s.responses) == 0 {
		return monitoring.SummarizeMetricsDataResponse{}, nil, errNoMockResponse
	}

	response := s.responses[0]
	s.responses = s.responses[1:]

	var next *string
	if len(s.tokens) > 0 {
		next = s.tokens[0]
		s.tokens = s.tokens[1:]
	}

	return response, next, nil
}

func stringPointer(value string) *string {
	return &value
}

func newJSONResponse(body string, headers http.Header) *http.Response {
	response := new(http.Response)
	response.StatusCode = http.StatusOK
	response.Header = headers.Clone()
	response.Body = io.NopCloser(strings.NewReader(body))
	response.ContentLength = int64(len(body))

	return response
}

func assertNextPageToken(t *testing.T, token *string, expected string) {
	t.Helper()

	if token == nil || *token != expected {
		t.Fatalf("expected next page token %q, got %#v", expected, token)
	}
}

func assertSummarizeRequest(t *testing.T, request *http.Request, expectedPage string) {
	t.Helper()

	if request == nil {
		t.Fatalf("expected request to be recorded")
	}

	requireEqual(t, request.URL.Path, "/metrics/actions/summarizeMetricsData", "request path")
	requireEqual(t, request.URL.Query().Get("page"), expectedPage, "page query")
}

func assertSummaryDatapoint(
	t *testing.T,
	summary monitoring.SummarizeMetricsDataResponse,
	expectedTimestamp time.Time,
	expectedValue float64,
) {
	t.Helper()

	if len(summary.Items) != 1 {
		t.Fatalf("expected one metric item, got %d", len(summary.Items))
	}

	datapoints := summary.Items[0].AggregatedDatapoints
	if len(datapoints) != 1 {
		t.Fatalf("expected one datapoint, got %d", len(datapoints))
	}

	requireEqual(t, datapoints[0].Timestamp.Time, expectedTimestamp, "datapoint timestamp")
	requireEqual(t, float32(*datapoints[0].Value), float32(expectedValue), "datapoint value")
}

func overrideInstancePrincipalProvider(
	t *testing.T,
	provider func() (common.ConfigurationProvider, error),
) {
	t.Helper()

	instancePrincipalProviderMu.Lock()

	providerOverrideSeq++
	overrideID := providerOverrideSeq

	providerOverrides = append(
		providerOverrides,
		providerOverride{id: overrideID, fn: provider},
	)
	instancePrincipalProviderFn = provider

	instancePrincipalProviderMu.Unlock()

	t.Cleanup(func() {
		instancePrincipalProviderMu.Lock()

		for i := range providerOverrides {
			if providerOverrides[i].id == overrideID {
				providerOverrides = append(
					providerOverrides[:i],
					providerOverrides[i+1:]...,
				)

				break
			}
		}

		if n := len(providerOverrides); n > 0 {
			instancePrincipalProviderFn = providerOverrides[n-1].fn
		} else {
			instancePrincipalProviderFn = defaultInstancePrincipalProvider
		}

		instancePrincipalProviderMu.Unlock()
	})
}

func overrideNewMonitoringClient(
	t *testing.T,
	constructor func(common.ConfigurationProvider) (monitoring.MonitoringClient, error),
) {
	t.Helper()

	newMonitoringClientMu.Lock()

	monitoringOverrideSeq++
	overrideID := monitoringOverrideSeq

	monitoringOverrides = append(
		monitoringOverrides,
		monitoringOverride{id: overrideID, fn: constructor},
	)
	newMonitoringClientFn = constructor

	newMonitoringClientMu.Unlock()

	t.Cleanup(func() {
		newMonitoringClientMu.Lock()

		for i := range monitoringOverrides {
			if monitoringOverrides[i].id == overrideID {
				monitoringOverrides = append(
					monitoringOverrides[:i],
					monitoringOverrides[i+1:]...,
				)

				break
			}
		}

		if n := len(monitoringOverrides); n > 0 {
			newMonitoringClientFn = monitoringOverrides[n-1].fn
		} else {
			newMonitoringClientFn = defaultNewMonitoringClientFn
		}

		newMonitoringClientMu.Unlock()
	})
}

type providerOverride struct {
	id uint64
	fn func() (common.ConfigurationProvider, error)
}

type monitoringOverride struct {
	id uint64
	fn func(common.ConfigurationProvider) (monitoring.MonitoringClient, error)
}

func stubConfigurationProvider(t *testing.T) fakeConfigurationProvider {
	t.Helper()

	key := testPrivateKey(t)

	return fakeConfigurationProvider{key: key}
}

var (
	testKeyOnce sync.Once       //nolint:gochecknoglobals
	testKey     *rsa.PrivateKey //nolint:gochecknoglobals
	errTestKey  error           //nolint:gochecknoglobals
)

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	testKeyOnce.Do(func() {
		testKey, errTestKey = rsa.GenerateKey(rand.Reader, 2048)
	})

	if errTestKey != nil {
		t.Fatalf("generate test RSA key: %v", errTestKey)
	}

	return testKey
}

type fakeConfigurationProvider struct {
	key *rsa.PrivateKey
}

func (f fakeConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return f.key, nil
}

func (f fakeConfigurationProvider) KeyID() (string, error) {
	return "ocid1.tenancy.oc1..test/ocid1.user.oc1..test/fingerprint", nil
}

func (f fakeConfigurationProvider) TenancyOCID() (string, error) {
	return "ocid1.tenancy.oc1..test", nil
}

func (f fakeConfigurationProvider) UserOCID() (string, error) {
	return "ocid1.user.oc1..test", nil
}

func (f fakeConfigurationProvider) KeyFingerprint() (string, error) {
	return "fingerprint", nil
}

func (f fakeConfigurationProvider) Region() (string, error) {
	return "us-phoenix-1", nil
}

func (f fakeConfigurationProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{
		AuthType:         common.AuthenticationType("instance_principal"),
		IsFromConfigFile: false,
		OboToken:         nil,
	}, nil
}

type stubAPICaller struct {
	response    *http.Response
	err         error
	lastRequest *http.Request
}

func newStubAPICaller(response *http.Response, err error) *stubAPICaller {
	return &stubAPICaller{response: response, err: err, lastRequest: nil}
}

func (s *stubAPICaller) Call(
	_ context.Context,
	req *http.Request,
) (*http.Response, error) {
	s.lastRequest = req

	if s.err != nil {
		return nil, s.err
	}

	return s.response, nil
}
