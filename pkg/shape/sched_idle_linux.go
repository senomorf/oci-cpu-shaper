//go:build linux && rootful

package shape

import (
	"sync"

	"golang.org/x/sys/unix"
)

var (
	schedSetSchedulerMu sync.RWMutex
	schedSetScheduler   = unix.SchedSetScheduler
)

func trySchedIdle() error {
	schedSetSchedulerMu.RLock()
	fn := schedSetScheduler
	schedSetSchedulerMu.RUnlock()

	return fn(0, unix.SCHED_IDLE, &unix.SchedParam{})
}
