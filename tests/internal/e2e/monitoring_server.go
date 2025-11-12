package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const defaultMonitoringValue = 0.25

// MonitoringResponse describes the payload returned to CLI queries.
// Status codes >=400 signal transient failures and are surfaced as errors by the client.
type MonitoringResponse struct {
	Status int
	Value  float64
	Body   string
}

// MonitoringRequest captures a single request observed by the fake Monitoring service.
type MonitoringRequest struct {
	ResourceID string
}

// MonitoringServer provides a lightweight HTTP interface that mimics the OCI Monitoring API
// enough for the CLI to exercise the adaptive controller in tests.
type MonitoringServer struct {
	server *httptest.Server

	mu        sync.Mutex
	requests  []MonitoringRequest
	responses []MonitoringResponse
	next      int
}

// StartMonitoringServer starts a fake Monitoring server that replays the provided responses.
// When more requests than responses are received, the final response is repeated.
func StartMonitoringServer(tb testing.TB, responses []MonitoringResponse) *MonitoringServer {
	tb.Helper()

	srv := new(MonitoringServer)
	srv.responses = append(srv.responses, responses...)

	handler := http.HandlerFunc(srv.handleRequest(tb))

	server := httptest.NewServer(handler)
	tb.Cleanup(server.Close)

	srv.server = server

	return srv
}

// URL exposes the base URL for the fake Monitoring server.
func (s *MonitoringServer) URL() string {
	if s == nil || s.server == nil {
		return ""
	}

	return s.server.URL
}

// Requests returns a snapshot of the requests observed so far.
func (s *MonitoringServer) Requests() []MonitoringRequest {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := make([]MonitoringRequest, len(s.requests))
	copy(snapshot, s.requests)

	return snapshot
}

func (s *MonitoringServer) handleRequest(tb testing.TB) func(http.ResponseWriter, *http.Request) {
	tb.Helper()

	return func(writer http.ResponseWriter, req *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		resourceID := req.URL.Query().Get("resource")
		s.requests = append(s.requests, MonitoringRequest{ResourceID: resourceID})

		var resp MonitoringResponse
		if len(s.responses) == 0 {
			resp = MonitoringResponse{
				Status: http.StatusOK,
				Value:  defaultMonitoringValue,
				Body:   "",
			}
		} else {
			if s.next < len(s.responses) {
				resp = s.responses[s.next]
				s.next++
			} else {
				resp = s.responses[len(s.responses)-1]
			}
		}

		status := resp.Status
		if status == 0 {
			status = http.StatusOK
		}

		if status != http.StatusOK {
			body := resp.Body
			if body == "" {
				body = http.StatusText(status)
			}

			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))

			return
		}

		payload := monitoringPayload{Value: resp.Value}

		writer.Header().Set("Content-Type", "application/json")

		encodeErr := json.NewEncoder(writer).Encode(&payload)
		if encodeErr != nil {
			tb.Fatalf("encode monitoring payload: %v", encodeErr)
		}
	}
}
