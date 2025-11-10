//nolint:testpackage // tests require access to internal helpers
package adapt

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/est"
)

var (
	errNoResultsConfigured = errors.New("test: no results configured")
	errOCIDown             = errors.New("test: oci down")
)

type metricResult struct {
	value float64
	err   error
}

type controllerScenario struct {
	name         string
	results      []metricResult
	expectations []stepExpectation
}

type stepExpectation struct {
	state        State
	target       float64
	nextInterval time.Duration
}

type controllerStepper interface {
	step(ctx context.Context) time.Duration
}

type fakeMetrics struct {
	results   []metricResult
	callIndex int
	mu        sync.Mutex
}

func newFakeMetrics(results []metricResult) *fakeMetrics {
	copied := make([]metricResult, len(results))
	copy(copied, results)

	return &fakeMetrics{results: copied, callIndex: 0, mu: sync.Mutex{}}
}

func (f *fakeMetrics) QueryP95CPU(ctx context.Context, _ string) (float64, error) {
	if len(f.results) == 0 {
		return 0, errNoResultsConfigured
	}

	err := ctx.Err()
	if err != nil {
		return 0, fmt.Errorf("query p95 context: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.callIndex >= len(f.results) {
		last := f.results[len(f.results)-1]

		return last.value, last.err
	}

	result := f.results[f.callIndex]
	f.callIndex++

	return result.value, result.err
}

type fakeShaper struct {
	target float64
	calls  []float64
}

func newFakeShaper() *fakeShaper {
	return &fakeShaper{target: 0, calls: make([]float64, 0)}
}

func (f *fakeShaper) SetTarget(v float64) {
	f.target = v
	f.calls = append(f.calls, v)
}

func (f *fakeShaper) Target() float64 { return f.target }

func TestControllerStateTransitions(t *testing.T) {
	t.Parallel()

	scenarios := []controllerScenario{
		{
			name: "success then fallback recovery",
			results: []metricResult{
				{value: 0.20, err: nil},
				{value: 0, err: errOCIDown},
				{value: 0.29, err: nil},
			},
			expectations: []stepExpectation{
				{state: StateNormal, target: 0.27, nextInterval: time.Hour},
				{state: StateFallback, target: 0.25, nextInterval: time.Hour},
				{state: StateNormal, target: 0.25, nextInterval: 6 * time.Hour},
			},
		},
		{
			name: "clamps within bounds",
			results: []metricResult{
				{value: 0.10, err: nil},
				{value: 0.50, err: nil},
			},
			expectations: []stepExpectation{
				{state: StateNormal, target: 0.27, nextInterval: time.Hour},
				{state: StateNormal, target: 0.26, nextInterval: 6 * time.Hour},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runControllerScenario(t, scenario)
		})
	}
}

func runControllerScenario(t *testing.T, scenario controllerScenario) {
	t.Helper()

	metrics := newFakeMetrics(scenario.results)
	shaper := newFakeShaper()
	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	cfg.RelaxedInterval = 6 * time.Hour

	controller, err := NewAdaptiveController(cfg, metrics, nil, shaper)
	if err != nil {
		t.Fatalf("NewAdaptiveController: %v", err)
	}

	if diff := math.Abs(shaper.Target() - cfg.FallbackTarget); diff > 1e-9 {
		t.Fatalf(
			"expected initial fallback target %.2f got %.2f",
			cfg.FallbackTarget,
			shaper.Target(),
		)
	}

	stepper, ok := any(controller).(controllerStepper)
	if !ok {
		t.Fatalf("controller does not expose stepper interface")
	}

	for stepIndex, expectation := range scenario.expectations {
		interval := stepper.step(context.Background())

		if controller.State() != expectation.state {
			t.Fatalf(
				"step %d state: got %v want %v",
				stepIndex,
				controller.State(),
				expectation.state,
			)
		}

		if diff := math.Abs(controller.Target() - expectation.target); diff > 1e-9 {
			t.Fatalf(
				"step %d target mismatch: got %.2f want %.2f",
				stepIndex,
				controller.Target(),
				expectation.target,
			)
		}

		if interval != expectation.nextInterval {
			t.Fatalf(
				"step %d interval: got %v want %v",
				stepIndex,
				interval,
				expectation.nextInterval,
			)
		}
	}
}

func TestAdaptiveControllerRunLifecycle(t *testing.T) {
	t.Parallel()

	metrics := newFakeMetrics([]metricResult{{value: 0.24, err: nil}, {value: 0.26, err: nil}})
	shaper := newFakeShaper()
	cfg := DefaultConfig()
	cfg.Interval = 5 * time.Millisecond
	cfg.RelaxedInterval = 10 * time.Millisecond
	cfg.Mode = "  enforce  "
	cfg.ResourceID = "resource"

	estimator := &fakeEstimator{
		observations: []est.Observation{
			{
				Timestamp:    time.Unix(0, 0),
				Utilisation:  0.5,
				BusyJiffies:  0,
				TotalJiffies: 0,
				Err:          nil,
			},
		},
		consumed: atomic.Int32{},
	}

	controller, err := NewAdaptiveController(cfg, metrics, estimator, shaper)
	if err != nil {
		t.Fatalf("NewAdaptiveController: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- controller.Run(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error: %v", err)
	}

	if controller.Mode() != "enforce" {
		t.Fatalf("unexpected mode: %q", controller.Mode())
	}

	if controller.LastP95() == 0 {
		t.Fatalf("expected last p95 to be recorded")
	}

	if estimator.consumed.Load() == 0 {
		t.Fatalf("expected estimator observations to be consumed")
	}
}

func TestNewNoopController(t *testing.T) {
	t.Parallel()

	ctrl := NewNoopController("  noop-mode  ")
	if ctrl.Mode() != "noop-mode" {
		t.Fatalf("unexpected mode: %q", ctrl.Mode())
	}

	err := ctrl.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

type fakeEstimator struct {
	observations []est.Observation
	consumed     atomic.Int32
}

func (f *fakeEstimator) Run(context.Context) <-chan est.Observation {
	observationsCh := make(chan est.Observation, len(f.observations))
	for _, observation := range f.observations {
		observationsCh <- observation

		f.consumed.Add(1)
	}

	close(observationsCh)

	return observationsCh
}
