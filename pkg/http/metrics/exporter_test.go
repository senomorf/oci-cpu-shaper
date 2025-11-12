package metrics_test

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metrics "oci-cpu-shaper/pkg/http/metrics"
)

const openMetricsContentType = "application/openmetrics-text; version=1.0.0; charset=utf-8"

var errFailingWriter = errors.New("metrics: failing writer")

func TestExporterRenderProducesOpenMetrics(t *testing.T) {
	t.Parallel()

	exporter := metrics.NewExporter()
	exporter.SetMode(" dry-run ")
	exporter.SetState(" fallback ")
	exporter.SetTarget(0.275)
	exporter.ObserveOCIP95(0.33, time.Unix(1_700_001_234, 0))
	exporter.SetDutyCycle(1500 * time.Microsecond)
	exporter.SetWorkerCount(4)
	exporter.ObserveHostCPU(0.6789)

	body, err := exporter.Render()
	if err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	got := string(body)
	expected := strings.Join([]string{
		"# HELP shaper_target_ratio Target duty cycle ratio assigned to worker pool.",
		"# TYPE shaper_target_ratio gauge",
		"shaper_target_ratio 0.275000",
		"# HELP shaper_mode Controller operating mode (value set to 1 for the active mode).",
		"# TYPE shaper_mode gauge",
		"shaper_mode{mode=\"dry-run\"} 1",
		"# HELP shaper_state Controller state machine output (value set to 1 for the active state).",
		"# TYPE shaper_state gauge",
		"shaper_state{state=\"fallback\"} 1",
		"# HELP oci_p95 Last observed OCI CPU P95 ratio.",
		"# TYPE oci_p95 gauge",
		"oci_p95 0.330000",
		"# HELP oci_last_success_epoch Unix epoch seconds of the last successful OCI metrics query.",
		"# TYPE oci_last_success_epoch counter",
		"oci_last_success_epoch 1700001234",
		"# HELP duty_cycle_ms Duty cycle quantum configured for workers (milliseconds).",
		"# TYPE duty_cycle_ms gauge",
		"duty_cycle_ms 1.500",
		"# HELP worker_count Number of worker goroutines consuming CPU.",
		"# TYPE worker_count gauge",
		"worker_count 4",
		"# HELP host_cpu_percent Last recorded host CPU utilisation percentage.",
		"# TYPE host_cpu_percent gauge",
		"host_cpu_percent 67.89",
		"# EOF",
		"",
	}, "\n")

	if got != expected {
		t.Fatalf("unexpected metrics output:\nexpected:\n%s\n\nactual:\n%s", expected, got)
	}
}

func TestExporterServeHTTPWritesContentType(t *testing.T) {
	t.Parallel()

	exporter := metrics.NewExporter()
	exporter.SetMode("noop")
	exporter.SetState("normal")

	recorder := httptest.NewRecorder()
	exporter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != 200 {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}

	if got := recorder.Header().Get("Content-Type"); got != openMetricsContentType {
		t.Fatalf("unexpected content type: %q", got)
	}
}

func TestExporterWriteToPropagatesWriterErrors(t *testing.T) {
	t.Parallel()

	exporter := metrics.NewExporter()
	exporter.SetMode("noop")

	_, err := exporter.WriteTo(failingWriter{})
	if err == nil {
		t.Fatal("expected error from WriteTo")
	}

	if !strings.Contains(err.Error(), "write metrics") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestExporterGuardsAgainstInvalidInputs(t *testing.T) {
	t.Parallel()

	exporter := metrics.NewExporter()
	exporter.SetMode("")
	exporter.SetState(" ")
	exporter.SetTarget(math.NaN())
	exporter.ObserveOCIP95(-10, time.Time{})
	exporter.SetDutyCycle(-time.Second)
	exporter.SetWorkerCount(-5)
	exporter.ObserveHostCPU(math.Inf(1))

	data, err := exporter.Render()
	if err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "shaper_mode{mode=\"unknown\"} 1") {
		t.Fatalf("expected unknown mode, got %s", output)
	}

	if !strings.Contains(output, "shaper_state{state=\"unknown\"} 1") {
		t.Fatalf("expected unknown state, got %s", output)
	}

	if !strings.Contains(output, "shaper_target_ratio 0.000000") {
		t.Fatalf("expected clamped target, got %s", output)
	}

	if !strings.Contains(output, "worker_count 0") {
		t.Fatalf("expected worker_count clamped to zero, got %s", output)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailingWriter
}
