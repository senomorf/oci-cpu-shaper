package metrics

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	contentType           = "application/openmetrics-text; version=1.0.0; charset=utf-8"
	millisecondsPerSecond = 1000.0
	hundredPercent        = 100.0
)

var errNilWriter = errors.New("metrics: writer is nil")

// Exporter tracks controller and estimator metrics and exposes them via HTTP.
type Exporter struct {
	mu sync.RWMutex

	shaperTarget    float64
	shaperMode      string
	shaperState     string
	ociP95          float64
	ociLastSuccess  time.Time
	dutyCycleMillis float64
	workerCount     float64
	hostCPUPercent  float64
}

// NewExporter constructs an Exporter with zeroed metrics.
func NewExporter() *Exporter {
	return new(Exporter)
}

// SetMode records the controller mode label.
func (e *Exporter) SetMode(mode string) {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		trimmed = "unknown"
	}

	e.mu.Lock()
	e.shaperMode = trimmed
	e.mu.Unlock()
}

// SetState records the current controller state label.
func (e *Exporter) SetState(state string) {
	trimmed := strings.TrimSpace(state)
	if trimmed == "" {
		trimmed = "unknown"
	}

	e.mu.Lock()
	e.shaperState = trimmed
	e.mu.Unlock()
}

// SetTarget stores the current duty-cycle target ratio.
func (e *Exporter) SetTarget(target float64) {
	if math.IsNaN(target) || math.IsInf(target, 0) {
		target = 0
	}

	clamped := math.Max(0, math.Min(1, target))

	e.mu.Lock()
	e.shaperTarget = clamped
	e.mu.Unlock()
}

// ObserveOCIP95 captures the most recent OCI P95 ratio and the time it was fetched.
func (e *Exporter) ObserveOCIP95(value float64, fetchedAt time.Time) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}

	if value < 0 {
		value = 0
	}

	e.mu.Lock()

	e.ociP95 = value
	if !fetchedAt.IsZero() {
		e.ociLastSuccess = fetchedAt
	}

	e.mu.Unlock()
}

// SetDutyCycle stores the worker duty-cycle quantum in milliseconds.
func (e *Exporter) SetDutyCycle(duration time.Duration) {
	millis := duration.Seconds() * millisecondsPerSecond
	if millis < 0 || math.IsNaN(millis) || math.IsInf(millis, 0) {
		millis = 0
	}

	e.mu.Lock()
	e.dutyCycleMillis = millis
	e.mu.Unlock()
}

// SetWorkerCount records the number of active worker goroutines.
func (e *Exporter) SetWorkerCount(count int) {
	value := float64(count)
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}

	e.mu.Lock()
	e.workerCount = value
	e.mu.Unlock()
}

// ObserveHostCPU records the latest host CPU utilisation percentage.
func (e *Exporter) ObserveHostCPU(utilisation float64) {
	if math.IsNaN(utilisation) || math.IsInf(utilisation, 0) {
		utilisation = 0
	}

	if utilisation < 0 {
		utilisation = 0
	}

	percent := utilisation * hundredPercent
	if percent > hundredPercent {
		percent = hundredPercent
	}

	e.mu.Lock()
	e.hostCPUPercent = percent
	e.mu.Unlock()
}

// ServeHTTP implements http.Handler for the metrics exporter.
func (e *Exporter) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	data, err := e.Render()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", contentType)
	_, _ = writer.Write(data)
}

// Render returns the current metrics snapshot encoded as OpenMetrics text.
func (e *Exporter) Render() ([]byte, error) {
	var buffer bytes.Buffer

	_, err := e.WriteTo(&buffer)
	if err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// WriteTo writes the current metrics snapshot to the provided writer.
func (e *Exporter) WriteTo(dst io.Writer) (int64, error) {
	if dst == nil {
		return 0, errNilWriter
	}

	snapshot := e.snapshot()

	lines := []string{
		"# HELP shaper_target_ratio Target duty cycle ratio assigned to worker pool.\n",
		"# TYPE shaper_target_ratio gauge\n",
		fmt.Sprintf("shaper_target_ratio %.6f\n", snapshot.shaperTarget),
		"# HELP shaper_mode Controller operating mode (value set to 1 for the active mode).\n",
		"# TYPE shaper_mode gauge\n",
		fmt.Sprintf("shaper_mode{mode=\"%s\"} 1\n", snapshot.shaperMode),
		"# HELP shaper_state Controller state machine output (value set to 1 for the active state).\n",
		"# TYPE shaper_state gauge\n",
		fmt.Sprintf("shaper_state{state=\"%s\"} 1\n", snapshot.shaperState),
		"# HELP oci_p95 Last observed OCI CPU P95 ratio.\n",
		"# TYPE oci_p95 gauge\n",
		fmt.Sprintf("oci_p95 %.6f\n", snapshot.ociP95),
		"# HELP oci_last_success_epoch Unix epoch seconds of the last successful OCI metrics query.\n",
		"# TYPE oci_last_success_epoch counter\n",
		fmt.Sprintf("oci_last_success_epoch %.0f\n", snapshot.ociLastSuccessEpoch),
		"# HELP duty_cycle_ms Duty cycle quantum configured for workers (milliseconds).\n",
		"# TYPE duty_cycle_ms gauge\n",
		fmt.Sprintf("duty_cycle_ms %.3f\n", snapshot.dutyCycleMillis),
		"# HELP worker_count Number of worker goroutines consuming CPU.\n",
		"# TYPE worker_count gauge\n",
		fmt.Sprintf("worker_count %.0f\n", snapshot.workerCount),
		"# HELP host_cpu_percent Last recorded host CPU utilisation percentage.\n",
		"# TYPE host_cpu_percent gauge\n",
		fmt.Sprintf("host_cpu_percent %.2f\n", snapshot.hostCPUPercent),
		"# EOF\n",
	}

	var total int64

	for _, line := range lines {
		n, err := io.WriteString(dst, line)

		total += int64(n)
		if err != nil {
			return total, fmt.Errorf("write metrics: %w", err)
		}
	}

	return total, nil
}

type exporterSnapshot struct {
	shaperTarget        float64
	shaperMode          string
	shaperState         string
	ociP95              float64
	ociLastSuccessEpoch float64
	dutyCycleMillis     float64
	workerCount         float64
	hostCPUPercent      float64
}

func (e *Exporter) snapshot() exporterSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	epoch := 0.0
	if !e.ociLastSuccess.IsZero() {
		epoch = float64(e.ociLastSuccess.Unix())
	}

	return exporterSnapshot{
		shaperTarget:        e.shaperTarget,
		shaperMode:          e.shaperMode,
		shaperState:         e.shaperState,
		ociP95:              e.ociP95,
		ociLastSuccessEpoch: epoch,
		dutyCycleMillis:     e.dutyCycleMillis,
		workerCount:         e.workerCount,
		hostCPUPercent:      e.hostCPUPercent,
	}
}
