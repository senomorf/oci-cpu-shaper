package e2eclient

import (
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"oci-cpu-shaper/pkg/adapt"
)

type loggingRecorder struct {
	logger   *zap.Logger
	delegate adapt.MetricsRecorder

	mu        sync.Mutex
	lastState string
}

// NewLoggingRecorder decorates the provided MetricsRecorder so e2e tests can observe
// controller state transitions via structured logs.
//
//nolint:ireturn // tests rely on interface for decorator wiring
func NewLoggingRecorder(
	logger *zap.Logger,
	delegate adapt.MetricsRecorder,
) adapt.MetricsRecorder {
	if logger == nil || delegate == nil {
		return delegate
	}

	return &loggingRecorder{ //nolint:exhaustruct // zero-value fields are intentional
		logger:   logger,
		delegate: delegate,
	}
}

func (r *loggingRecorder) SetMode(mode string) {
	if r.delegate != nil {
		r.delegate.SetMode(mode)
	}
}

func (r *loggingRecorder) SetState(state string) {
	trimmed := strings.TrimSpace(state)
	if r.delegate != nil {
		r.delegate.SetState(trimmed)
	}

	r.mu.Lock()

	previous := r.lastState
	if trimmed != previous {
		r.logger.Info(
			"controller state transition",
			zap.String("from", previous),
			zap.String("to", trimmed),
		)
		r.lastState = trimmed
	}

	r.mu.Unlock()
}

func (r *loggingRecorder) SetTarget(target float64) {
	if r.delegate != nil {
		r.delegate.SetTarget(target)
	}
}

func (r *loggingRecorder) ObserveOCIP95(value float64, fetchedAt time.Time) {
	if r.delegate != nil {
		r.delegate.ObserveOCIP95(value, fetchedAt)
	}
}

func (r *loggingRecorder) ObserveHostCPU(utilisation float64) {
	if r.delegate != nil {
		r.delegate.ObserveHostCPU(utilisation)
	}
}
