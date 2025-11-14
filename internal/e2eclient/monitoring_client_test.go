//nolint:testpackage // white-box tests exercise internal seams for coverage.
package e2eclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"oci-cpu-shaper/pkg/oci"
)

func TestNewMonitoringClientValidatesEndpoint(t *testing.T) {
	t.Parallel()

	_, err := NewMonitoringClient("   ")
	if !errors.Is(err, errMonitoringEndpointRequired) {
		t.Fatalf("expected errMonitoringEndpointRequired, got %v", err)
	}
}

//nolint:cyclop // multiple request/response paths validated in one scenario.
func TestMonitoringClientQueryP95CPUScenarios(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Query().Get("resource") {
			case "empty":
				writer.WriteHeader(http.StatusNoContent)
			case "error":
				writer.WriteHeader(http.StatusServiceUnavailable)
				_, _ = writer.Write([]byte("backend unavailable"))
			case "invalid":
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("not-json"))
			default:
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{"value":0.42}`))
			}
		}),
	)
	t.Cleanup(server.Close)

	client, err := NewMonitoringClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected client error: %v", err)
	}

	_, err = client.QueryP95CPU(context.Background(), "empty")
	if !errors.Is(err, oci.ErrNoMetricsData) {
		t.Fatalf("expected ErrNoMetricsData, got %v", err)
	}

	_, err = client.QueryP95CPU(context.Background(), "error")
	if err == nil || !strings.Contains(err.Error(), "backend unavailable") {
		t.Fatalf("expected backend error, got %v", err)
	}

	_, err = client.QueryP95CPU(context.Background(), "invalid")
	if err == nil || !strings.Contains(err.Error(), "decode payload") {
		t.Fatalf("expected decode error, got %v", err)
	}

	value, err := client.QueryP95CPU(context.Background(), "ok")
	if err != nil {
		t.Fatalf("unexpected success error: %v", err)
	}

	if value != 0.42 {
		t.Fatalf("unexpected value: got %.2f want 0.42", value)
	}
}

func TestMonitoringClientRejectsUninitialisedHTTPClient(t *testing.T) {
	t.Parallel()

	client := &monitoringClient{endpoint: "http://127.0.0.1", http: nil}

	_, err := client.QueryP95CPU(context.Background(), "resource")
	if !errors.Is(err, errMonitoringHTTPNotInitialised) {
		t.Fatalf("expected errMonitoringHTTPNotInitialised, got %v", err)
	}
}
