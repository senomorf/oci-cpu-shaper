package adapt

import (
	"context"
	"errors"
	"testing"
	"time"
)

type metricResult struct {
	value float64
	err   error
}

type fakeMetrics struct {
	results   []metricResult
	callIndex int
}

func (f *fakeMetrics) QueryP95CPU(ctx context.Context, resourceID string) (float64, error) {
	if len(f.results) == 0 {
		return 0, errors.New("no results configured")
	}
	if f.callIndex >= len(f.results) {
		res := f.results[len(f.results)-1]
		return res.value, res.err
	}
	res := f.results[f.callIndex]
	f.callIndex++
	return res.value, res.err
}

type fakeShaper struct {
	target float64
	calls  []float64
}

func (f *fakeShaper) SetTarget(v float64) {
	f.target = v
	f.calls = append(f.calls, v)
}

func (f *fakeShaper) Target() float64 { return f.target }

func TestControllerStateTransitions(t *testing.T) {
	tests := []struct {
		name       string
		results    []metricResult
		wantStates []State
		wantTarget []float64
		wantNext   []time.Duration
	}{
		{
			name: "success then fallback recovery",
			results: []metricResult{
				{value: 0.20},
				{err: errors.New("oci down")},
				{value: 0.29},
			},
			wantStates: []State{StateNormal, StateFallback, StateNormal},
			wantTarget: []float64{0.27, 0.25, 0.25},
			wantNext:   []time.Duration{time.Hour, time.Hour, 6 * time.Hour},
		},
		{
			name: "clamps within bounds",
			results: []metricResult{
				{value: 0.10},
				{value: 0.50},
			},
			wantStates: []State{StateNormal, StateNormal},
			wantTarget: []float64{0.27, 0.26},
			wantNext:   []time.Duration{time.Hour, 6 * time.Hour},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &fakeMetrics{results: tc.results}
			shaper := &fakeShaper{}
			cfg := DefaultConfig
			cfg.Interval = time.Hour
			cfg.RelaxedInterval = 6 * time.Hour

			controller, err := NewAdaptiveController(cfg, metrics, nil, shaper)
			if err != nil {
				t.Fatalf("NewAdaptiveController: %v", err)
			}

			if shaper.Target() != cfg.FallbackTarget {
				t.Fatalf("expected initial fallback target %v got %v", cfg.FallbackTarget, shaper.Target())
			}

			for i, wantState := range tc.wantStates {
				next := controller.step(context.Background())
				if controller.State() != wantState {
					t.Fatalf("step %d state: got %v want %v", i, controller.State(), wantState)
				}
				gotTarget := controller.Target()
				if gotTarget != tc.wantTarget[i] {
					t.Fatalf("step %d target: got %.2f want %.2f", i, gotTarget, tc.wantTarget[i])
				}
				if next != tc.wantNext[i] {
					t.Fatalf("step %d interval: got %v want %v", i, next, tc.wantNext[i])
				}
			}
		})
	}
}
