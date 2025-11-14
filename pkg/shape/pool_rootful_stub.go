//go:build !rootful

package shape

func configureRootfulHooks(pool *Pool) {
	if pool == nil {
		return
	}
}
