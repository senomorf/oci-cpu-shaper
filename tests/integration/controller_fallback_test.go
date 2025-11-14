//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"oci-cpu-shaper/internal/e2eclient"
	"oci-cpu-shaper/pkg/adapt"
	metricshttp "oci-cpu-shaper/pkg/http/metrics"
	interne2e "oci-cpu-shaper/tests/internal/e2e"
)

func TestControllerFallbackRecoversAfterMonitoringGap(t *testing.T) {
	observerCore, observed := observer.New(zap.InfoLevel)
	logger := zap.New(observerCore)

	exporter := metricshttp.NewExporter()
	recorder := e2eclient.NewLoggingRecorder(logger, exporter)

	monitoring := interne2e.StartMonitoringServer(t, []interne2e.MonitoringResponse{
		{Status: http.StatusNoContent},
		{Value: 0.28},
	})

	metricsClient, err := e2eclient.NewMonitoringClient(monitoring.URL())
	if err != nil {
		t.Fatalf("create monitoring client: %v", err)
	}

	cfg := adapt.DefaultConfig()
	cfg.ResourceID = "ocid1.instance.oc1..integration"
	cfg.Interval = 200 * time.Millisecond
	cfg.RelaxedInterval = 200 * time.Millisecond
	cfg.FallbackTarget = 0.25

	shaper := newRecordingShaper()

	controller, err := adapt.NewAdaptiveController(cfg, metricsClient, nil, shaper, recorder)
	if err != nil {
		t.Fatalf("create adaptive controller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- controller.Run(ctx)
	}()

	waitForTransition(t, observed, "", "fallback", 2*time.Second)
	waitForTransition(t, observed, "fallback", "normal", 4*time.Second)

	cancel()

	runErr := <-errCh
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		t.Fatalf("controller run returned error: %v", runErr)
	}

	requests := monitoring.Requests()
	if len(requests) < 2 {
		t.Fatalf("expected at least two monitoring requests, saw %d", len(requests))
	}

	metrics, err := exporter.Render()
	if err != nil {
		t.Fatalf("render metrics: %v", err)
	}

	assertMetricContains(t, metrics, fmt.Sprintf("shaper_state{state=\"%s\"} 1", controller.State()))
	assertNonZeroMetric(t, metrics, "oci_last_success_epoch")
	assertMetricContains(t, metrics, fmt.Sprintf("oci_p95 %.6f", controller.LastP95()))

	if got := shaper.Target(); got != cfg.FallbackTarget && got != controller.Target() {
		t.Fatalf("unexpected shaper target %.2f (controller target %.2f)", got, controller.Target())
	}
}

func waitForTransition(t *testing.T, observed *observer.ObservedLogs, from, to string, timeout time.Duration) {
	t.Helper()

	deadline := time.After(timeout)
	for {
		entries := observed.TakeAll()
		for _, entry := range entries {
			if entry.Message != "controller state transition" {
				continue
			}

			if entry.ContextMap()["from"] == from && entry.ContextMap()["to"] == to {
				return
			}
		}

		select {
		case <-deadline:
			t.Fatalf("expected transition from %q to %q not observed", from, to)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func assertMetricContains(t *testing.T, metrics []byte, expected string) {
	t.Helper()

	if !bytes.Contains(metrics, []byte(expected)) {
		t.Fatalf("expected metrics to contain %q\nmetrics:\n%s", expected, metrics)
	}
}

func assertNonZeroMetric(t *testing.T, metrics []byte, name string) {
	t.Helper()

	prefix := []byte(name + " ")
	index := bytes.Index(metrics, prefix)
	if index == -1 {
		t.Fatalf("metric %q not found in\n%s", name, metrics)
	}

	remainder := metrics[index+len(prefix):]
	end := bytes.IndexByte(remainder, '\n')
	if end == -1 {
		end = len(remainder)
	}

	value := strings.TrimSpace(string(remainder[:end]))
	if value == "" || value == "0" || value == "0.0" {
		t.Fatalf("expected metric %q to be non-zero, got %q\nmetrics:\n%s", name, value, metrics)
	}
}

type recordingShaper struct {
	mu     sync.Mutex
	target float64
}

func newRecordingShaper() *recordingShaper {
	return &recordingShaper{}
}

func (r *recordingShaper) SetTarget(target float64) {
	r.mu.Lock()
	r.target = target
	r.mu.Unlock()
}

func (r *recordingShaper) Target() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.target
}
