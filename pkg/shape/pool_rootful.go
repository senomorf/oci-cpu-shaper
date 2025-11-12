//go:build rootful

package shape

func configureRootfulHooks(pool *Pool) {
	pool.workerStartHook = trySchedIdle
}
