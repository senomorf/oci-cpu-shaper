//nolint:testpackage // white-box tests exercise internal seams for coverage.
package e2eclient

import (
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"oci-cpu-shaper/pkg/adapt"
)

func TestNewLoggingRecorderDelegatesWhenMissingInputs(t *testing.T) {
	t.Parallel()

	var recorder adapt.MetricsRecorder

	if got := NewLoggingRecorder(nil, nil); got != nil {
		t.Fatalf("expected nil recorder, got %v", got)
	}

	if got := NewLoggingRecorder(zap.NewNop(), recorder); got != recorder {
		t.Fatalf("expected delegate to be returned unchanged")
	}
}

//nolint:cyclop,funlen // exercise multiple branches in a single end-to-end scenario.
func TestLoggingRecorderForwardsCalls(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	delegate := newRecordingDelegate()

	recorder := NewLoggingRecorder(logger, delegate)
	if recorder == nil {
		t.Fatal("expected recorder")
	}

	const resourceID = "ocid1.instance.oc1..example"

	recorder.SetMode("observe")
	recorder.SetState(" fallback ")
	recorder.SetTarget(0.37)
	recorder.ObserveOCIP95(0.42, time.Unix(100, 0))
	recorder.ObserveHostCPU(0.55)

	if delegate.mode != "observe" {
		t.Fatalf("expected mode to be forwarded, got %q", delegate.mode)
	}

	if delegate.state != "fallback" {
		t.Fatalf("expected trimmed state, got %q", delegate.state)
	}

	if delegate.target != 0.37 {
		t.Fatalf("unexpected target: %.2f", delegate.target)
	}

	if delegate.ocip95 != 0.42 {
		t.Fatalf("unexpected ocip95 value %.2f", delegate.ocip95)
	}

	if delegate.hostCPU != 0.55 {
		t.Fatalf("unexpected host cpu %.2f", delegate.hostCPU)
	}

	entries := logs.FilterMessage("controller state transition").All()
	if len(entries) != 1 {
		t.Fatalf("expected one transition log, got %d", len(entries))
	}

	entry := entries[0]
	if from := entry.ContextMap()["from"]; from != "" {
		t.Fatalf("unexpected from value %v", from)
	}

	if to := entry.ContextMap()["to"]; to != "fallback" {
		t.Fatalf("unexpected to value %v", to)
	}

	// Ensure subsequent SetState does not emit duplicate logs.
	recorder.SetState("fallback")

	if logs.FilterMessage("controller state transition").Len() != 1 {
		t.Fatal("expected no additional transition logs")
	}

	// Cover nil delegate branch.
	if recorder := NewLoggingRecorder(logger, nil); recorder != nil {
		t.Fatalf("expected nil recorder when delegate missing")
	}

	// Ensure recorder handles multiple state transitions.
	recorder = NewLoggingRecorder(logger, delegate)
	recorder.SetState("normal")

	if logs.FilterMessage("controller state transition").Len() != 2 {
		t.Fatal("expected second transition log")
	}

	recorder.ObserveHostCPU(0.66)
	recorder.ObserveOCIP95(0.67, time.Unix(200, 0))

	if count := atomic.LoadInt64(&delegate.ocip95Count); count != 2 {
		t.Fatalf("expected ocip95 to be observed twice, got %d", count)
	}

	delegate.lastResource = resourceID
}

type recordingDelegate struct {
	mode         string
	state        string
	target       float64
	ocip95       float64
	hostCPU      float64
	lastResource string
	ocip95Count  int64
}

func newRecordingDelegate() *recordingDelegate {
	return &recordingDelegate{} //nolint:exhaustruct // zero-value setup suffices for tests
}

func (r *recordingDelegate) SetMode(mode string) {
	r.mode = mode
}

func (r *recordingDelegate) SetState(state string) {
	r.state = state
}

func (r *recordingDelegate) SetTarget(target float64) {
	r.target = target
}

func (r *recordingDelegate) ObserveOCIP95(value float64, _ time.Time) {
	r.ocip95 = value
	atomic.AddInt64(&r.ocip95Count, 1)
}

func (r *recordingDelegate) ObserveHostCPU(utilisation float64) {
	r.hostCPU = utilisation
}
