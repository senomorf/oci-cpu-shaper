package shape

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPoolAppliesDutyCycle(t *testing.T) {
	pool, err := NewPool(1, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var mu sync.Mutex
	var busyDurations []time.Duration
	var sleepDurations []time.Duration

	pool.busyFunc = func(d time.Duration) {
		mu.Lock()
		busyDurations = append(busyDurations, d)
		mu.Unlock()
	}
	pool.sleepFunc = func(d time.Duration) {
		mu.Lock()
		sleepDurations = append(sleepDurations, d)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	pool.SetTarget(0.4)

	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(2 * time.Millisecond)

	mu.Lock()
	if len(busyDurations) == 0 {
		t.Fatalf("expected busy durations to be recorded")
	}
	if len(busyDurations) != len(sleepDurations) {
		t.Fatalf("busy and sleep slices should match in length")
	}

	for i := range busyDurations {
		if busyDurations[i] <= 0 {
			t.Fatalf("expected positive busy duration")
		}
		if busyDurations[i] >= 5*time.Millisecond {
			t.Fatalf("busy duration should be less than quantum: got %v", busyDurations[i])
		}
		if sleepDurations[i] <= 0 {
			t.Fatalf("expected positive sleep duration")
		}
		if busyDurations[i]+sleepDurations[i] != 5*time.Millisecond {
			t.Fatalf("quantum not preserved: busy %v sleep %v", busyDurations[i], sleepDurations[i])
		}
	}
	mu.Unlock()
}

func TestPoolYieldsUnderZeroTarget(t *testing.T) {
	pool, err := NewPool(1, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var yieldCount int
	pool.yieldFunc = func() {
		yieldCount++
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	pool.SetTarget(0)

	time.Sleep(5 * time.Millisecond)
	cancel()
	time.Sleep(2 * time.Millisecond)

	if yieldCount == 0 {
		t.Fatalf("expected yields when target is zero")
	}
}
