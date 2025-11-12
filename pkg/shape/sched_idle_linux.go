//go:build linux && rootful

package shape

import (
        "sync"
        "unsafe"

        "golang.org/x/sys/unix"
)

type schedParam struct {
        priority int32
}

var (
        schedSetSchedulerMu sync.RWMutex
        schedSetScheduler   = func(pid int, policy int, param *schedParam) error {
                if param == nil {
                        param = &schedParam{}
                }

                _, _, errno := unix.Syscall6(
                        unix.SYS_SCHED_SETSCHEDULER,
                        uintptr(pid),
                        uintptr(policy),
                        uintptr(unsafe.Pointer(param)),
                        0,
                        0,
                        0,
                )
                if errno != 0 {
                        return errno
                }

                return nil
        }
)

func trySchedIdle() error {
        schedSetSchedulerMu.RLock()
        fn := schedSetScheduler
        schedSetSchedulerMu.RUnlock()

        return fn(0, unix.SCHED_IDLE, &schedParam{})
}
