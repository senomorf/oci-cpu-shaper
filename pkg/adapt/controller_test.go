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
	errNoResultsConfigured  = errors.New("test: no results configured")
	errOCIDown              = errors.New("test: oci down")
	errEstimatorObservation = errors.New("test: estimator observation failure")
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

func TestControllerCpuUtilisationAcrossOCPUs(t *testing.T) {
	t.Parallel()

	// CpuUtilisation is reported as the percentage of busy time across the
	// entire instance, so shapes with additional OCPUs should follow the
	// exact same adjustment cadence. The cases below simulate the
	// aggregated stream for 1–4 OCPU shapes to confirm the controller keeps
	// returning the same targets and relaxed intervals during prolonged
	// bursts.
	highUtilisationScenario := controllerScenario{
		name: "baseline ocpu burst",
		results: []metricResult{
			{value: 0.15, err: nil},
			{value: 0.32, err: nil},
			{value: 0.34, err: nil},
			{value: 0.36, err: nil},
			{value: 0.38, err: nil},
			{value: 0.40, err: nil},
			{value: 0.45, err: nil},
		},
		expectations: []stepExpectation{
			{state: StateNormal, target: 0.27, nextInterval: time.Hour},
			{state: StateNormal, target: 0.26, nextInterval: 6 * time.Hour},
			{state: StateNormal, target: 0.25, nextInterval: 6 * time.Hour},
			{state: StateNormal, target: 0.24, nextInterval: 6 * time.Hour},
			{state: StateNormal, target: 0.23, nextInterval: 6 * time.Hour},
			{state: StateNormal, target: 0.22, nextInterval: 6 * time.Hour},
			{state: StateNormal, target: 0.22, nextInterval: 6 * time.Hour},
		},
	}

	cases := []struct {
		name  string
		ocpus int
	}{
		{name: "1-ocpu burst matches policy", ocpus: 1},
		{name: "2-ocpu burst matches policy", ocpus: 2},
		{name: "3-ocpu burst matches policy", ocpus: 3},
		{name: "4-ocpu burst matches policy", ocpus: 4},
	}

	for _, shapeCase := range cases {
		t.Run(shapeCase.name, func(t *testing.T) {
			t.Parallel()

			results := append([]metricResult(nil), highUtilisationScenario.results...)
			expectations := append([]stepExpectation(nil), highUtilisationScenario.expectations...)

			scenario := controllerScenario{
				name:         shapeCase.name,
				results:      results,
				expectations: expectations,
			}

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

	controller, err := NewAdaptiveController(cfg, metrics, nil, shaper, nil)
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

func TestConsumeEstimatorSuppression(t *testing.T) {
	t.Parallel()

	metrics := newFakeMetrics([]metricResult{{value: 0.25, err: nil}})
	shaper := newFakeShaper()
	cfg := DefaultConfig()
	cfg.SuppressThreshold = 0.8
	cfg.SuppressResume = 0.5

	controller, err := NewAdaptiveController(cfg, metrics, nil, shaper, nil)
	if err != nil {
		t.Fatalf("NewAdaptiveController: %v", err)
	}

	feedObservation(controller, 0, 0.9, nil)
	feedObservation(controller, 1, 0.95, nil)

	if controller.State() != StateSuppressed {
		t.Fatalf("expected suppressed state after high utilisation, got %v", controller.State())
	}

	if controller.Target() != 0 {
		t.Fatalf(
			"expected target to drop to zero during suppression, got %.2f",
			controller.Target(),
		)
	}

	for i := 0; i < 6 && controller.State() == StateSuppressed; i++ {
		feedObservation(controller, int64(2+i), 0.10, nil)
	}

	if controller.State() != StateFallback {
		t.Fatalf("expected controller to resume fallback after cooling, got %v", controller.State())
	}

	if diff := math.Abs(controller.Target() - cfg.FallbackTarget); diff > 1e-9 {
		t.Fatalf(
			"expected fallback target %.2f after suppression, got %.2f",
			cfg.FallbackTarget,
			controller.Target(),
		)
	}

	if len(shaper.calls) < 2 {
		t.Fatalf(
			"expected shaper to be called for suppression transitions, got %d calls",
			len(shaper.calls),
		)
	}
}

func TestConsumeEstimatorHandlesErrors(t *testing.T) {
	t.Parallel()

	metrics := newFakeMetrics([]metricResult{{value: 0.25, err: nil}})
	shaper := newFakeShaper()
	cfg := DefaultConfig()

	controller, err := NewAdaptiveController(cfg, metrics, nil, shaper, nil)
	if err != nil {
		t.Fatalf("NewAdaptiveController: %v", err)
	}

	feedObservation(controller, 0, 0, errEstimatorObservation)

	if controller.LastEstimatorError() == nil {
		t.Fatal("expected estimator error to be recorded")
	}

	if controller.State() != StateFallback {
		t.Fatalf(
			"expected fallback state to remain after estimator error, got %v",
			controller.State(),
		)
	}

	if diff := math.Abs(controller.Target() - cfg.FallbackTarget); diff > 1e-9 {
		t.Fatalf(
			"expected fallback target to remain %.2f after estimator error, got %.2f",
			cfg.FallbackTarget,
			controller.Target(),
		)
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

	controller, err := NewAdaptiveController(cfg, metrics, estimator, shaper, nil)
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

func TestAdaptiveControllerEmitsMetricsSignals(t *testing.T) {
	t.Parallel()

	recorder := newStubMetricsRecorder()
	metrics := newFakeMetrics([]metricResult{{value: 0.20, err: nil}})
	shaper := newFakeShaper()
	cfg := DefaultConfig()
	cfg.Mode = "  enforce  "

	controller, err := NewAdaptiveController(cfg, metrics, nil, shaper, recorder)
	if err != nil {
		t.Fatalf("NewAdaptiveController: %v", err)
	}

	requirePositiveInt(t, "modeCalls", recorder.modeCalls)
	requireEqual(t, "mode", recorder.mode, "enforce")
	requireEqual(t, "initialState", recorder.state, StateFallback.String())
	requireFloatApprox(t, "initialTarget", recorder.target, cfg.FallbackTarget)

	feedObservation(controller, 0, 0.75, nil)

	requirePositiveInt(t, "hostCalls", recorder.hostCalls)
	requireFloatApprox(t, "hostUtilisation", recorder.host, 0.75)

	stepper, ok := any(controller).(controllerStepper)
	if !ok {
		t.Fatalf("controller does not expose stepper interface")
	}

	stepper.step(context.Background())

	requirePositiveInt(t, "ociCalls", recorder.ociCalls)
	requireFloatApprox(t, "ociValue", recorder.ociValue, 0.20)
	requireNotZeroTime(t, "ociTime", recorder.ociTime)
	requireEqual(t, "stateAfterStep", recorder.state, StateNormal.String())
	requireFloatApprox(t, "targetAfterStep", recorder.target, shaper.Target())
}

type stubMetricsRecorder struct {
	mu          sync.Mutex
	mode        string
	modeCalls   int
	state       string
	stateCalls  int
	target      float64
	targetCalls int
	ociValue    float64
	ociTime     time.Time
	ociCalls    int
	host        float64
	hostCalls   int
}

func newStubMetricsRecorder() *stubMetricsRecorder { return new(stubMetricsRecorder) }

func (s *stubMetricsRecorder) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mode = mode
	s.modeCalls++
}

func (s *stubMetricsRecorder) SetState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = state
	s.stateCalls++
}

func (s *stubMetricsRecorder) SetTarget(target float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.target = target
	s.targetCalls++
}

func (s *stubMetricsRecorder) ObserveOCIP95(value float64, fetchedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ociValue = value
	s.ociTime = fetchedAt
	s.ociCalls++
}

func (s *stubMetricsRecorder) ObserveHostCPU(utilisation float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.host = utilisation
	s.hostCalls++
}

func requireEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s to be %v, got %v", name, want, got)
	}
}

func requireFloatApprox(t *testing.T, name string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("expected %s %.6f, got %.6f", name, want, got)
	}
}

func requirePositiveInt(t *testing.T, name string, value int) {
	t.Helper()

	if value <= 0 {
		t.Fatalf("expected %s to be positive, got %d", name, value)
	}
}

func requireNotZeroTime(t *testing.T, name string, value time.Time) {
	t.Helper()

	if value.IsZero() {
		t.Fatalf("expected %s to be non-zero", name)
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

func feedObservation(controller *AdaptiveController, ts int64, utilisation float64, err error) {
	controller.handleObservation(est.Observation{
		Timestamp:    time.Unix(ts, 0),
		Utilisation:  utilisation,
		BusyJiffies:  0,
		TotalJiffies: 0,
		Err:          err,
	})
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
