//nolint:testpackage // tests require access to unexported hooks
package shape

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errTestSchedIdleDenied = errors.New("sched idle denied")

func TestPoolAppliesDutyCycle(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(1, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var (
		metricsMu      sync.Mutex
		busyDurations  []time.Duration
		sleepDurations []time.Duration
	)

	pool.busyFunc = func(d time.Duration) {
		metricsMu.Lock()

		busyDurations = append(busyDurations, d)

		metricsMu.Unlock()
	}
	pool.sleepFunc = func(d time.Duration) {
		metricsMu.Lock()

		sleepDurations = append(sleepDurations, d)

		metricsMu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	pool.SetTarget(0.4)

	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(2 * time.Millisecond)

	metricsMu.Lock()
	defer metricsMu.Unlock()

	assertBusyAndSleepDurations(t, busyDurations, sleepDurations, 5*time.Millisecond)
}

func assertBusyAndSleepDurations(
	t *testing.T,
	busyDurations []time.Duration,
	sleepDurations []time.Duration,
	quantum time.Duration,
) {
	t.Helper()

	if len(busyDurations) == 0 {
		t.Fatalf("expected busy durations to be recorded")
	}

	if len(busyDurations) != len(sleepDurations) {
		t.Fatalf("busy and sleep slices should match in length")
	}

	for index := range busyDurations {
		if busyDurations[index] <= 0 {
			t.Fatalf("expected positive busy duration")
		}

		if busyDurations[index] >= quantum {
			t.Fatalf("busy duration should be less than quantum: got %v", busyDurations[index])
		}

		if sleepDurations[index] <= 0 {
			t.Fatalf("expected positive sleep duration")
		}

		if busyDurations[index]+sleepDurations[index] != quantum {
			t.Fatalf(
				"quantum not preserved: busy %v sleep %v",
				busyDurations[index],
				sleepDurations[index],
			)
		}
	}
}

func TestPoolYieldsUnderZeroTarget(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(1, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var yieldCount atomic.Int64

	pool.yieldFunc = func() {
		yieldCount.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	pool.SetTarget(0)

	time.Sleep(5 * time.Millisecond)
	cancel()
	time.Sleep(2 * time.Millisecond)

	if yieldCount.Load() == 0 {
		t.Fatalf("expected yields when target is zero")
	}
}

func TestBusyWaitHandlesDurations(t *testing.T) {
	t.Parallel()

	start := time.Now()

	busyWait(0)

	if elapsed := time.Since(start); elapsed > time.Millisecond {
		t.Fatalf("busyWait should return immediately for zero duration, took %v", elapsed)
	}

	start = time.Now()

	busyWait(200 * time.Microsecond)

	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("busyWait exceeded expected duration, took %v", elapsed)
	}
}

func TestPoolWorkerStartHookSuccess(t *testing.T) {
	t.Parallel()

	const workers = 3

	pool, err := NewPool(workers, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var (
		hookCount        atomic.Int32
		handlerCount     atomic.Int32
		workerStartGroup sync.WaitGroup
	)

	workerStartGroup.Add(workers)

	pool.workerStartHook = func() error {
		hookCount.Add(1)
		workerStartGroup.Done()

		return nil
	}
	pool.workerStartErrorHandler = func(error) {
		handlerCount.Add(1)
	}
	pool.sleepFunc = func(time.Duration) {}
	pool.yieldFunc = func() {}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)

	done := make(chan struct{})

	go func() {
		workerStartGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("timeout waiting for worker start hook")
	}

	cancel()
	time.Sleep(2 * time.Millisecond)

	if got := hookCount.Load(); got != workers {
		t.Fatalf("expected hook count %d, got %d", workers, got)
	}

	if got := handlerCount.Load(); got != 0 {
		t.Fatalf("expected no error handler invocations, got %d", got)
	}
}

//nolint:funlen // integration-style test ensures handler runs per worker
func TestPoolWorkerStartHookErrorPropagates(t *testing.T) {
	t.Parallel()

	const workers = 2

	pool, err := NewPool(workers, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var (
		hookCount    atomic.Int32
		handlerCount atomic.Int32
	)

	hookErr := errTestSchedIdleDenied

	var handlerWG sync.WaitGroup
	handlerWG.Add(workers)

	pool.workerStartHook = func() error {
		hookCount.Add(1)

		return hookErr
	}
	pool.workerStartErrorHandler = func(err error) {
		if !errors.Is(err, hookErr) {
			t.Errorf("unexpected error propagated: %v", err)
		}

		handlerCount.Add(1)
		handlerWG.Done()
	}
	pool.sleepFunc = func(time.Duration) {}
	pool.yieldFunc = func() {}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)

	done := make(chan struct{})

	go func() {
		handlerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("timeout waiting for worker start error handler")
	}

	cancel()
	time.Sleep(2 * time.Millisecond)

	if got := hookCount.Load(); got != workers {
		t.Fatalf("expected hook count %d, got %d", workers, got)
	}

	if got := handlerCount.Load(); got != workers {
		t.Fatalf("expected handler count %d, got %d", workers, got)
	}
}
