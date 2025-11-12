//go:build linux && rootful

package shape

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestTrySchedIdleSuccess(t *testing.T) {
	t.Parallel()

	schedSetSchedulerMu.Lock()
	original := schedSetScheduler
	schedSetSchedulerMu.Unlock()

	t.Cleanup(func() {
		schedSetSchedulerMu.Lock()
		schedSetScheduler = original
		schedSetSchedulerMu.Unlock()
	})

	var called bool
	schedSetSchedulerMu.Lock()
	schedSetScheduler = func(pid int, policy int, param *unix.SchedParam) error {
		called = true

		if pid != 0 {
			t.Fatalf("expected pid 0, got %d", pid)
		}

		if policy != unix.SCHED_IDLE {
			t.Fatalf("expected SCHED_IDLE policy, got %d", policy)
		}

		if param == nil {
			t.Fatalf("expected non-nil sched param")
		}

		return nil
	}
	schedSetSchedulerMu.Unlock()

	if err := trySchedIdle(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Fatalf("expected schedSetScheduler to be called")
	}
}

func TestTrySchedIdleEPERM(t *testing.T) {
	t.Parallel()

	schedSetSchedulerMu.Lock()
	original := schedSetScheduler
	schedSetSchedulerMu.Unlock()

	t.Cleanup(func() {
		schedSetSchedulerMu.Lock()
		schedSetScheduler = original
		schedSetSchedulerMu.Unlock()
	})

	schedSetSchedulerMu.Lock()
	schedSetScheduler = func(int, int, *unix.SchedParam) error {
		return unix.EPERM
	}
	schedSetSchedulerMu.Unlock()

	err := trySchedIdle()

	if !errors.Is(err, unix.EPERM) {
		t.Fatalf("expected EPERM, got %v", err)
	}
}
