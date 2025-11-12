//go:build !linux || !rootful

package shape

func trySchedIdle() error {
	return nil
}
