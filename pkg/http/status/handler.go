package status

import (
	"encoding/json"
	"net/http"

	"oci-cpu-shaper/pkg/adapt"
)

// Controller exposes the status surface required by the health handler.
type Controller interface {
	State() adapt.State
	LastError() error
	LastEstimatorError() error
}

// Snapshot captures the controller status returned by the handler.
type Snapshot struct {
	State          string `json:"state"`
	LastOCIError   string `json:"ociError"`
	EstimatorError string `json:"estimatorError"`
}

// Handler renders controller health information as JSON.
type Handler struct {
	controller Controller
}

// NewHandler constructs a Handler that proxies controller status.
func NewHandler(controller Controller) *Handler {
	return &Handler{controller: controller}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	if h == nil || h.controller == nil {
		http.Error(writer, "controller unavailable", http.StatusServiceUnavailable)

		return
	}

	snapshot := Snapshot{
		State:          h.controller.State().String(),
		LastOCIError:   "",
		EstimatorError: "",
	}

	lastOCIError := h.controller.LastError()
	if lastOCIError != nil {
		snapshot.LastOCIError = lastOCIError.Error()
	}

	estimatorErr := h.controller.LastEstimatorError()
	if estimatorErr != nil {
		snapshot.EstimatorError = estimatorErr.Error()
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		http.Error(writer, "marshal status", http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write(payload)
}
