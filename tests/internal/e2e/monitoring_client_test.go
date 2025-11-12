package e2e_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"oci-cpu-shaper/pkg/oci"
	interne2e "oci-cpu-shaper/tests/internal/e2e"
)

func TestMonitoringClientNoContentReturnsErrNoMetricsData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client, err := interne2e.NewMonitoringClient(server.URL)
	if err != nil {
		t.Fatalf("NewMonitoringClient returned error: %v", err)
	}

	value, err := client.QueryP95CPU(context.Background(), "ocid1.instance.oc1..example")
	if !errors.Is(err, oci.ErrNoMetricsData) {
		t.Fatalf("expected ErrNoMetricsData, got %v", err)
	}

	if value != 0 {
		t.Fatalf("expected zero value on ErrNoMetricsData, got %.2f", value)
	}
}
