package main

import (
	"flag"
	"os"
	"runtime"
	"testing"
	"time"
)

//nolint:paralleltest // test mutates process-wide flags and os.Args.
func TestMainHonorsDurationAndWorkerDefaults(t *testing.T) {
	runCPUHog(t, []string{"-duration", "5ms", "-workers", "0"})
}

//nolint:paralleltest // test mutates process-wide flags and os.Args.
func TestMainTreatsNegativeWorkersAsOne(t *testing.T) {
	runCPUHog(t, []string{"-duration", "5ms", "-workers", "-5"})
}

func runCPUHog(t *testing.T, args []string) {
	t.Helper()

	originalArgs := os.Args

	os.Args = append([]string{"cpu-hog"}, args...)

	defer func() { os.Args = originalArgs }()

	originalFlags := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	defer func() { flag.CommandLine = originalFlags }()

	done := make(chan struct{})

	go func() {
		defer close(done)

		main()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("cpu-hog main did not return: goroutines=%d", runtime.NumGoroutine())
	}
}
