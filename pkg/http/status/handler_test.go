package status_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"oci-cpu-shaper/pkg/adapt"
	status "oci-cpu-shaper/pkg/http/status"
)

var (
	errMetricsUnavailable = errors.New("metrics unavailable")
	errEstimatorStalled   = errors.New("estimator stalled")
)

type stubController struct {
	state  adapt.State
	ociErr error
	estErr error
}

func (s *stubController) State() adapt.State { return s.state }

func (s *stubController) LastError() error { return s.ociErr }

func (s *stubController) LastEstimatorError() error { return s.estErr }

func TestHandlerReturnsSnapshot(t *testing.T) {
	t.Parallel()

	controller := &stubController{
		state:  adapt.StateFallback,
		ociErr: errMetricsUnavailable,
		estErr: errEstimatorStalled,
	}

	handler := status.NewHandler(controller)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recorder.Code)
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var snapshot status.Snapshot

	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &snapshot)
	if decodeErr != nil {
		t.Fatalf("failed to decode response: %v", decodeErr)
	}

	if snapshot.State != adapt.StateFallback.String() {
		t.Fatalf("expected state %q, got %q", adapt.StateFallback.String(), snapshot.State)
	}

	if snapshot.LastOCIError != errMetricsUnavailable.Error() {
		t.Fatalf(
			"expected OCI error %q, got %q",
			errMetricsUnavailable.Error(),
			snapshot.LastOCIError,
		)
	}

	if snapshot.EstimatorError != errEstimatorStalled.Error() {
		t.Fatalf(
			"expected estimator error %q, got %q",
			errEstimatorStalled.Error(),
			snapshot.EstimatorError,
		)
	}
}

func TestHandlerWithoutControllerReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	handler := status.NewHandler(nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", recorder.Code)
	}
}
