//go:build !linux || !rootful

package shape

func trySchedIdle() error { //nolint:unused // only linked into non-rootful builds where it is never invoked
	return nil
}
