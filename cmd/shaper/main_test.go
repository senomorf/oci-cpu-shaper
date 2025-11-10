package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"oci-cpu-shaper/internal/buildinfo"
	"oci-cpu-shaper/pkg/adapt"
)

var (
	errStubLoggerBoom    = errors.New("logger failure")
	errStubControllerRun = errors.New("controller run failed")
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

func TestRunSuccessfulPath(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return buildinfo.Info{
			Version:   "test-version",
			GitCommit: "test-commit",
			BuildDate: "2024-05-01",
		}
	}
	deps.newLogger = func(level string) (*zap.Logger, error) {
		if level != "debug" {
			t.Fatalf("expected log level \"debug\", got %q", level)
		}

		return logger, nil
	}

	var ctrl stubController

	deps.newController = func(mode string) adapt.Controller {
		ctrl.mode = mode

		return &ctrl
	}

	exitCode := run(t.Context(), []string{"--mode", "enforce", "--log-level", "debug"}, deps, io.Discard)
	if exitCode != 0 {
		t.Fatalf("expected zero exit code, got %d", exitCode)
	}

	if !ctrl.runCalled {
		t.Fatal("expected controller Run to be called")
	}

	if ctrl.mode != "enforce" {
		t.Fatalf("expected controller mode \"enforce\", got %q", ctrl.mode)
	}

	assertInfoLogEntry(t, observed.All(), "test-version", "test-commit", "2024-05-01")
}

func TestRunReturnsParseErrorExitCode(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return buildinfo.Info{}
	}

	exitCode := run(t.Context(), []string{"--mode", "invalid"}, deps, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2 for parse errors, got %d", exitCode)
	}

	if got := stderr.String(); !strings.Contains(got, "unsupported mode") {
		t.Fatalf("expected error message about unsupported mode, got %q", got)
	}
}

func TestRunReturnsLoggerConfigurationError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return buildinfo.Info{}
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return nil, errStubLoggerBoom
	}

	exitCode := run(t.Context(), nil, deps, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 when logger configuration fails, got %d", exitCode)
	}

	if got := stderr.String(); !strings.Contains(got, "failed to configure logger") {
		t.Fatalf("expected logger configuration failure message, got %q", got)
	}
}

func TestRunHandlesControllerError(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	var ctrl stubController

	ctrl.runErr = errStubControllerRun

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return buildinfo.Info{Version: "test-version"}
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return logger, nil
	}

	deps.newController = func(mode string) adapt.Controller {
		ctrl.mode = mode

		return &ctrl
	}

	exitCode := run(t.Context(), []string{"--mode", "noop", "--log-level", "debug"}, deps, io.Discard)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 when controller.Run returns an error, got %d", exitCode)
	}

	if !ctrl.runCalled {
		t.Fatal("expected controller Run to be invoked")
	}

	failureEntries := observed.FilterMessage("controller execution failed").All()
	if len(failureEntries) == 0 {
		t.Fatalf("expected controller failure log, got %+v", observed.All())
	}
}

func assertInfoLogEntry(t *testing.T, entries []observer.LoggedEntry, version, commit, date string) {
	t.Helper()

	var infoEntry *observer.LoggedEntry

	for i := range entries {
		if entries[i].Message == "starting oci-cpu-shaper" {
			infoEntry = &entries[i]
			break
		}
	}

	if infoEntry == nil {
		t.Fatalf("expected info log entry, got %+v", entries)
	}

	if got := fieldString(infoEntry.Context, "version"); got != version {
		t.Fatalf("expected version field %q, got %q", version, got)
	}

	if got := fieldString(infoEntry.Context, "commit"); got != commit {
		t.Fatalf("expected commit field %q, got %q", commit, got)
	}

	if got := fieldString(infoEntry.Context, "buildDate"); got != date {
		t.Fatalf("expected buildDate field %q, got %q", date, got)
	}
}

type stubController struct {
	mode      string
	runErr    error
	runCalled bool
}

func (c *stubController) Run(ctx context.Context) error {
	c.runCalled = true
	_ = ctx

	return c.runErr
}

func (c *stubController) Mode() string {
	return c.mode
}

func fieldString(fields []zap.Field, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.String
		}
	}

	return ""
}
