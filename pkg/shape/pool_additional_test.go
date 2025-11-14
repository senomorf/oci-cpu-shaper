//nolint:testpackage // coverage for internal hooks requires direct access.
package shape

import (
	"math"
	"testing"
	"time"
)

func TestPoolWorkersAndQuantumAccessors(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(3, 2*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := pool.Workers(); got != 3 {
		t.Fatalf("unexpected worker count: got %d want 3", got)
	}

	if got := pool.Quantum(); got != 2*time.Millisecond {
		t.Fatalf("unexpected quantum: got %s want %s", got, 2*time.Millisecond)
	}
}

func TestPoolSetTargetBoundsInput(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pool.SetTarget(1.5)

	if got := pool.Target(); got != 1 {
		t.Fatalf("expected target to clamp to 1, got %.2f", got)
	}

	pool.SetTarget(-0.2)

	if got := pool.Target(); got != 0 {
		t.Fatalf("expected negative target to clamp to 0, got %.2f", got)
	}

	pool.SetTarget(math.NaN())

	if got := pool.Target(); got != 0 {
		t.Fatalf("expected NaN target to reset to 0, got %.2f", got)
	}
}

func TestConfigureRootfulHooksNoop(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(1, DefaultQuantum)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configureRootfulHooks(pool)

	configureRootfulHooks(nil)
}

func TestTrySchedIdleNoop(t *testing.T) {
	t.Parallel()

	err := trySchedIdle()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestNewPoolRejectsNonPositiveWorkerCount(t *testing.T) {
	t.Parallel()

	_, err := NewPool(0, DefaultQuantum)
	if err == nil {
		t.Fatal("expected error when worker count is non-positive")
	}
}

func TestNewPoolClampsQuantumWithinBounds(t *testing.T) {
	t.Parallel()

	tooSmall, err := NewPool(1, time.Microsecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tooSmall.Quantum(); got != minQuantum {
		t.Fatalf("expected quantum to clamp to %s, got %s", minQuantum, got)
	}

	tooLarge, err := NewPool(1, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tooLarge.Quantum(); got != maxQuantum {
		t.Fatalf("expected quantum to clamp to %s, got %s", maxQuantum, got)
	}
}
