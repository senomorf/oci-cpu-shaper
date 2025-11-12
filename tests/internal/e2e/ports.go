package e2e

import (
	"context"
	"net"
	"testing"
)

// FreePort allocates an ephemeral TCP port bound to localhost and returns it after closing the listener.
func FreePort(tb testing.TB) int {
	tb.Helper()

	var listenCfg net.ListenConfig

	listener, err := listenCfg.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("allocate free port: %v", err)
	}

	defer func() {
		_ = listener.Close()
	}()

	addr := listener.Addr().(*net.TCPAddr) //nolint:forcetypeassert // listener is tcp

	return addr.Port
}
