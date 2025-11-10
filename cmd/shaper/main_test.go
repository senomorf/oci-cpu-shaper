package main

import (
	"errors"
	"flag"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestParseArgsDefaults(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if opts.configPath != "/etc/oci-cpu-shaper/config.yaml" {
		t.Fatalf("expected default config path, got %q", opts.configPath)
	}
	if opts.logLevel != "info" {
		t.Fatalf("expected default log level, got %q", opts.logLevel)
	}
	if opts.mode != "dry-run" {
		t.Fatalf("expected default mode, got %q", opts.mode)
	}
}

func TestParseArgsValidCustomizations(t *testing.T) {
	t.Parallel()

	args := []string{"--config", "./testdata/config.yaml", "--log-level", "debug", "--mode", "enforce"}
	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if opts.configPath != "./testdata/config.yaml" {
		t.Fatalf("unexpected config path: %q", opts.configPath)
	}
	if opts.logLevel != "debug" {
		t.Fatalf("unexpected log level: %q", opts.logLevel)
	}
	if opts.mode != "enforce" {
		t.Fatalf("unexpected mode: %q", opts.mode)
	}
}

func TestParseArgsRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--mode", "observe"})
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}

func TestNewLoggerRejectsInvalidLevel(t *testing.T) {
	t.Parallel()

	_, err := newLogger("not-a-level")
	if err == nil {
		t.Fatal("expected error when creating logger with invalid level")
	}
}

func TestNewLoggerAppliesLevel(t *testing.T) {
	t.Parallel()

	logger, err := newLogger("debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	if !logger.Core().Enabled(zap.DebugLevel) {
		t.Fatal("expected logger to enable debug level")
	}
}

func TestParseArgsTrimSpaces(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs([]string{"--mode", "  NOOP ", "--log-level", " info "})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if opts.mode != "noop" {
		t.Fatalf("expected trimmed lowercase mode, got %q", opts.mode)
	}
	if opts.logLevel != "info" {
		t.Fatalf("expected trimmed log level, got %q", opts.logLevel)
	}
}

func TestParseArgsReturnsFlagError(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parsing error")
	}
	if !errors.Is(err, flag.ErrHelp) && !strings.Contains(err.Error(), "flag provided but not defined") {
		// Accept either standard flag error or ErrHelp depending on flag parsing behavior.
		t.Fatalf("unexpected error type: %v", err)
	}
}
