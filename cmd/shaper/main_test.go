package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"oci-cpu-shaper/internal/buildinfo"
	"oci-cpu-shaper/pkg/adapt"
	"oci-cpu-shaper/pkg/imds"
	"oci-cpu-shaper/pkg/oci"
)

var (
	errStubLoggerBoom    = errors.New("logger failure")
	errStubControllerRun = errors.New("controller run failed")
	errRegionDown        = errors.New("region down")
	errInstanceDown      = errors.New("id down")
	errShapeDown         = errors.New("shape down")
)

const (
	maxUint32         = ^uint32(0)
	stubCompartmentID = "ocid1.compartment.oc1..test"
	imdsAuthHeaderKey = "Authorization"
	imdsAuthHeaderVal = "Bearer Oracle"
)

func TestParseArgsDefaults(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if opts.configPath != defaultConfigPath {
		t.Fatalf("expected default config path, got %q", opts.configPath)
	}

	if opts.logLevel != defaultLogLevel {
		t.Fatalf("expected default log level, got %q", opts.logLevel)
	}

	if opts.mode != modeDryRun {
		t.Fatalf("expected default mode, got %q", opts.mode)
	}

	if opts.shutdownAfter != 0 {
		t.Fatalf("expected shutdownAfter default to be 0, got %v", opts.shutdownAfter)
	}
}

func TestParseArgsValidCustomizations(t *testing.T) {
	t.Parallel()

	args := []string{
		"--config",
		"./testdata/config.yaml",
		"--log-level",
		"debug",
		"--mode",
		"enforce",
		"--shutdown-after",
		"45s",
	}

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

	if opts.mode != modeEnforce {
		t.Fatalf("unexpected mode: %q", opts.mode)
	}

	if opts.shutdownAfter != 45*time.Second {
		t.Fatalf("unexpected shutdownAfter: %v", opts.shutdownAfter)
	}
}

func TestParseArgsRejectsNegativeShutdownAfter(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--shutdown-after", "-5s"})
	if err == nil {
		t.Fatal("expected error for negative shutdown-after duration")
	}

	if !errors.Is(err, errInvalidShutdownAfter) {
		t.Fatalf("expected errInvalidShutdownAfter, got %v", err)
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

	if opts.mode != modeNoop {
		t.Fatalf("expected trimmed lowercase mode, got %q", opts.mode)
	}

	if opts.logLevel != defaultLogLevel {
		t.Fatalf("expected trimmed log level, got %q", opts.logLevel)
	}
}

func TestParseArgsReturnsFlagError(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parsing error")
	}

	if !errors.Is(err, flag.ErrHelp) &&
		!strings.Contains(err.Error(), "flag provided but not defined") {
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
		return stubBuildInfo("test-version", "test-commit", "2024-05-01")
	}
	deps.newLogger = func(level string) (*zap.Logger, error) {
		if level != "debug" {
			t.Fatalf("expected log level \"debug\", got %q", level)
		}

		return logger, nil
	}

	var ctrl stubController

	pool := new(stubPoolStarter)

	deps.loadConfig = loadConfigStub()

	deps.newController = func(
		ctx context.Context,
		mode string,
		cfg runtimeConfig,
		imdsClient imds.Client,
	) (adapt.Controller, poolStarter, error) {
		_ = ctx
		_ = cfg
		_ = imdsClient
		ctrl.mode = mode

		return &ctrl, pool, nil
	}

	exitCode := run(
		t.Context(),
		[]string{"--mode", "enforce", "--log-level", "debug"},
		deps,
		io.Discard,
	)
	if exitCode != 0 {
		t.Fatalf("expected zero exit code, got %d", exitCode)
	}

	if !ctrl.runCalled {
		t.Fatal("expected controller Run to be called")
	}

	if ctrl.mode != modeEnforce {
		t.Fatalf("expected controller mode \"enforce\", got %q", ctrl.mode)
	}

	if pool.startCount != 1 {
		t.Fatalf("expected pool Start to be called once, got %d", pool.startCount)
	}

	assertInfoLogEntry(t, observed.All(), "test-version", "test-commit", "2024-05-01")
}

func TestRunAppliesShutdownAfter(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return stubBuildInfo("test-version", "test-commit", "2024-05-01")
	}
	deps.newLogger = func(level string) (*zap.Logger, error) {
		if level != defaultLogLevel {
			t.Fatalf("expected default log level %q, got %q", defaultLogLevel, level)
		}

		return logger, nil
	}
	deps.loadConfig = loadConfigStub()

	ctrl := new(stubController)

	deps.newController = func(
		ctx context.Context,
		mode string,
		cfg runtimeConfig,
		imdsClient imds.Client,
	) (adapt.Controller, poolStarter, error) {
		_ = cfg
		_ = imdsClient

		controllerCtxDeadline, controllerCtxHasDeadline := ctx.Deadline()
		if !controllerCtxHasDeadline {
			t.Fatal("expected controller factory context to include deadline")
		}

		if time.Until(controllerCtxDeadline) <= 0 {
			t.Fatal("expected controller deadline to be in the future when factory executed")
		}

		ctrl.mode = mode

		return ctrl, nil, nil
	}

	exitCode := run(t.Context(), []string{"--shutdown-after", "200ms"}, deps, io.Discard)
	if exitCode != exitCodeSuccess {
		t.Fatalf("expected zero exit code, got %d", exitCode)
	}

	requireRunInvoked(t, ctrl)
	requireDeadlineCaptured(t, ctrl)

	entries := observed.FilterMessage("starting oci-cpu-shaper").All()
	requireShutdownDuration(t, entries, 200*time.Millisecond)
}

func TestRunHandlesContextShutdown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		runErr error
		reason string
	}{
		{
			name:   "deadline exceeded",
			runErr: fmt.Errorf("adaptive controller run: %w", context.DeadlineExceeded),
			reason: context.DeadlineExceeded.Error(),
		},
		{
			name:   "context canceled",
			runErr: fmt.Errorf("adaptive controller run: %w", context.Canceled),
			reason: context.Canceled.Error(),
		},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			runShutdownScenario(t, scenario.runErr, scenario.reason)
		})
	}
}

func TestRunReturnsParseErrorExitCode(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return stubBuildInfo("", "", "")
	}

	exitCode := run(t.Context(), []string{"--mode", "invalid"}, deps, &stderr)
	if exitCode != exitCodeParseError {
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
		return stubBuildInfo("", "", "")
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return nil, errStubLoggerBoom
	}

	exitCode := run(t.Context(), nil, deps, &stderr)
	if exitCode != exitCodeRuntimeError {
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
		return stubBuildInfo("test-version", "", "")
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return logger, nil
	}

	deps.loadConfig = loadConfigStub()

	deps.newController = func(
		ctx context.Context,
		mode string,
		cfg runtimeConfig,
		imdsClient imds.Client,
	) (adapt.Controller, poolStarter, error) {
		_ = ctx
		_ = cfg
		_ = imdsClient
		ctrl.mode = mode

		return &ctrl, nil, nil
	}

	exitCode := run(
		t.Context(),
		[]string{"--mode", "noop", "--log-level", "debug"},
		deps,
		io.Discard,
	)
	if exitCode != exitCodeRuntimeError {
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

func TestRunHandlesControllerFactoryError(t *testing.T) {
	t.Parallel()

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return stubBuildInfo("test-version", "", "")
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return zap.NewNop(), nil
	}
	deps.loadConfig = func(string) (runtimeConfig, error) {
		cfg := defaultRuntimeConfig()
		cfg.OCI.CompartmentID = stubCompartmentID

		return cfg, nil
	}
	deps.newController = func(context.Context, string, runtimeConfig, imds.Client) (adapt.Controller, poolStarter, error) {
		return nil, nil, errStubControllerRun
	}

	exitCode := run(t.Context(), []string{"--mode", "enforce"}, deps, io.Discard)
	if exitCode != exitCodeRuntimeError {
		t.Fatalf("expected runtime error exit code, got %d", exitCode)
	}
}

func TestDefaultControllerFactoryReturnsNoopForMode(t *testing.T) {
	t.Parallel()

	noopIMDS := new(stubIMDSClient)

	controller, pool, err := defaultControllerFactory(
		context.Background(),
		modeNoop,
		defaultRuntimeConfig(),
		noopIMDS,
	)
	if err != nil {
		t.Fatalf("defaultControllerFactory returned error: %v", err)
	}

	if pool != nil {
		t.Fatalf("expected no pool for noop mode, got %#v", pool)
	}

	if got := controller.Mode(); got != modeNoop {
		t.Fatalf("expected controller mode %q, got %q", modeNoop, got)
	}

	if _, ok := controller.(*adapt.NoopController); !ok {
		t.Fatalf("expected noop controller implementation, got %T", controller)
	}
}

func TestDefaultControllerFactoryBuildsAdaptiveController(t *testing.T) {
	t.Parallel()

	fakeMetrics := newStubMetricsClient()
	ctx := withMetricsClientFactory(
		context.Background(),
		func(string) (oci.MetricsClient, error) {
			return fakeMetrics, nil
		},
	)

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = "ocid1.compartment.oc1..controller"
	cfg.Pool.Workers = 1
	cfg.Estimator.Interval = 500 * time.Millisecond

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceID = "ocid1.instance.oc1..controller"

	controller, pool, err := defaultControllerFactory(
		ctx,
		modeEnforce,
		cfg,
		imdsClient,
	)
	if err != nil {
		t.Fatalf("defaultControllerFactory returned error: %v", err)
	}

	if pool == nil {
		t.Fatal("expected pool to be returned for adaptive controller")
	}

	if controller.Mode() != modeEnforce {
		t.Fatalf("expected enforce mode, got %q", controller.Mode())
	}
}

func TestDefaultControllerFactoryErrorsOnMissingCompartmentID(t *testing.T) {
	t.Parallel()

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = ""

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceID = "ocid1.instance.oc1..missing"

	_, _, err := defaultControllerFactory(
		context.Background(),
		modeDryRun,
		cfg,
		imdsClient,
	)
	if err == nil {
		t.Fatal("expected error when compartment ID is missing")
	}
}

func TestDefaultControllerFactoryPropagatesMetricsFailure(t *testing.T) {
	t.Parallel()

	ctx := withMetricsClientFactory(
		context.Background(),
		func(string) (oci.MetricsClient, error) {
			return nil, errStubControllerRun
		},
	)

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = "ocid1.compartment.oc1..metrics"

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceID = "ocid1.instance.oc1..metrics"

	_, _, err := defaultControllerFactory(
		ctx,
		modeDryRun,
		cfg,
		imdsClient,
	)
	if err == nil {
		t.Fatal("expected error when metrics client creation fails")
	}
}

func TestDefaultControllerFactoryPropagatesIMDSError(t *testing.T) {
	t.Parallel()

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = "ocid1.compartment.oc1..imds"

	failingIMDS := new(stubIMDSClient)
	failingIMDS.instanceErr = errInstanceDown

	_, _, err := defaultControllerFactory(
		context.Background(),
		modeDryRun,
		cfg,
		failingIMDS,
	)
	if err == nil {
		t.Fatal("expected error when instance lookup fails")
	}
}

func TestBuildAdaptiveControllerUsesConfiguredInstanceID(t *testing.T) {
	t.Parallel()

	stubMetrics := newStubMetricsClient()
	ctx := withMetricsClientFactory(
		context.Background(),
		func(compartmentID string) (oci.MetricsClient, error) {
			if compartmentID != testCompartmentOverride {
				t.Fatalf("unexpected compartment id: %s", compartmentID)
			}

			return stubMetrics, nil
		},
	)

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = testCompartmentOverride
	cfg.OCI.InstanceID = "  ocid1.instance.oc1..override  "
	cfg.Pool.Workers = 1

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceErr = errInstanceDown

	controller, pool, err := buildAdaptiveController(
		ctx,
		modeDryRun,
		cfg,
		imdsClient,
	)
	if err != nil {
		t.Fatalf("buildAdaptiveController returned error: %v", err)
	}

	if pool == nil {
		t.Fatal("expected worker pool to be initialized")
	}

	if controller.Mode() != modeDryRun {
		t.Fatalf("unexpected mode: %s", controller.Mode())
	}

	if imdsClient.instanceCalls != 0 {
		t.Fatalf("expected override to skip IMDS lookup, got %d calls", imdsClient.instanceCalls)
	}
}

func TestBuildAdaptiveControllerOfflineSkipsExternalDependencies(t *testing.T) {
	t.Parallel()

	ctx := withMetricsClientFactory(
		context.Background(),
		func(string) (oci.MetricsClient, error) {
			t.Fatal("expected offline mode to avoid metrics factory")

			return nil, errStubControllerRun
		},
	)

	cfg := defaultRuntimeConfig()
	cfg.Controller.TargetStart = 0.42
	cfg.OCI.CompartmentID = ""
	cfg.OCI.InstanceID = ""
	cfg.OCI.Offline = true

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceErr = errInstanceDown

	controller, pool, err := buildAdaptiveController(ctx, modeDryRun, cfg, imdsClient)
	if err != nil {
		t.Fatalf("buildAdaptiveController returned error: %v", err)
	}

	if pool == nil {
		t.Fatal("expected worker pool to be initialized")
	}

	if controller.Mode() != modeDryRun {
		t.Fatalf("unexpected mode: %s", controller.Mode())
	}
}

func TestMainSuccessDoesNotExit(t *testing.T) { //nolint:paralleltest // mutates process-wide state
	originalExit := exitProcess

	defer func() { exitProcess = originalExit }()

	exitCalled := false
	exitProcess = func(code int) {
		exitCalled = true

		if code != exitCodeSuccess {
			t.Fatalf("unexpected exit code: %d", code)
		}
	}

	originalArgs := os.Args

	defer func() { os.Args = originalArgs }()

	os.Args = []string{"oci-cpu-shaper", "--mode", "noop"}

	main()

	if exitCalled {
		t.Fatal("expected main to complete without invoking exit")
	}
}

func TestMainPropagatesNonZeroExitCode(t *testing.T) { //nolint:paralleltest // mutates global state
	originalExit := exitProcess

	defer func() { exitProcess = originalExit }()

	exitCodes := make(chan int, 1)
	exitProcess = func(code int) {
		exitCodes <- code
	}

	originalArgs := os.Args

	defer func() { os.Args = originalArgs }()

	os.Args = []string{"oci-cpu-shaper", "--mode", "invalid"}

	main()

	select {
	case code := <-exitCodes:
		if code != exitCodeParseError {
			t.Fatalf("expected exit code %d, got %d", exitCodeParseError, code)
		}
	default:
		t.Fatal("expected main to invoke exit with parse error code")
	}
}

func TestDefaultIMDSFactoryUsesEnvironmentEndpoint(t *testing.T) {
	responses := map[string]string{
		"/opc/v2/instance/region":       "us-chicago-1",
		"/opc/v2/instance/id":           "ocid1.instance.oc1..exampleuniqueID",
		"/opc/v2/instance/shape-config": `{"ocpus":2,"memoryInGBs":32}`,
	}

	server := newIPv4TestServer(
		t,
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			body, ok := responses[req.URL.Path]
			if !ok {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}

			_, _ = writer.Write([]byte(body))
		}),
	)
	t.Cleanup(server.Close)

	t.Setenv(imdsEndpointEnv, " "+server.URL+"/opc/v2 ")

	client := defaultIMDSFactory()

	ctx := context.Background()

	region, err := client.Region(ctx)
	if err != nil {
		t.Fatalf("Region() returned error: %v", err)
	}

	if region != "us-chicago-1" {
		t.Fatalf("unexpected region %q", region)
	}

	instanceID, err := client.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID() returned error: %v", err)
	}

	if instanceID != "ocid1.instance.oc1..exampleuniqueID" {
		t.Fatalf("unexpected instance ID %q", instanceID)
	}

	shape, err := client.ShapeConfig(ctx)
	if err != nil {
		t.Fatalf("ShapeConfig() returned error: %v", err)
	}

	if shape.OCPUs != 2 || shape.MemoryInGBs != 32 {
		t.Fatalf("unexpected shape config: %+v", shape)
	}
}

func TestLogIMDSMetadataEmitsDetails(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	client := newLoggingStubIMDS(
		"us-ashburn-1",
		nil,
		"us-ashburn-1",
		nil,
		"ocid1.instance.oc1..exampleuniqueID",
		nil,
		stubCompartmentID,
		nil,
		stubShapeConfig(4, 64),
		nil,
	)

	ctrl := new(stubController)
	ctrl.mode = modeDryRun

	logIMDSMetadata(context.Background(), logger, client, ctrl, "", false)

	entry := requireSingleDebugEntry(t, observed)
	requireLogFieldString(t, entry, "region", "us-ashburn-1")
	requireLogFieldString(t, entry, "canonicalRegion", "us-ashburn-1")
	requireLogFieldString(t, entry, "instanceID", "ocid1.instance.oc1..exampleuniqueID")
	requireLogFieldString(t, entry, "compartmentID", stubCompartmentID)
	requireLogFieldFloat(t, entry, "shapeOCPUs", 4)
	requireLogFieldFloat(t, entry, "shapeMemoryGB", 64)
}

func TestLogIMDSMetadataWarnsOnFailures(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	client := newLoggingStubIMDS(
		"",
		errRegionDown,
		"",
		errRegionDown,
		"",
		errInstanceDown,
		"",
		errInstanceDown,
		stubShapeConfig(0, 0),
		errShapeDown,
	)

	ctrl := new(stubController)
	ctrl.mode = modeNoop

	logIMDSMetadata(context.Background(), logger, client, ctrl, "", false)

	warns := observed.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != 5 {
		t.Fatalf("expected five warnings, got %d", len(warns))
	}
}

func TestLogIMDSMetadataUsesOverrideInstanceID(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	client := newLoggingStubIMDS(
		"us-chicago-1",
		nil,
		"us-chicago-1",
		nil,
		"",
		nil,
		stubCompartmentID,
		nil,
		stubShapeConfig(2, 32),
		nil,
	)

	ctrl := new(stubController)
	ctrl.mode = modeDryRun

	logIMDSMetadata(
		context.Background(),
		logger,
		client,
		ctrl,
		"  ocid1.instance.oc1..override  ",
		false,
	)

	if client.instanceCalls != 0 {
		t.Fatalf(
			"expected override to skip IMDS instance lookup, got %d calls",
			client.instanceCalls,
		)
	}

	if client.canonicalRegionCalls == 0 {
		t.Fatalf("expected canonical region lookup when logging metadata")
	}

	if client.compartmentCalls == 0 {
		t.Fatalf("expected compartment lookup when logging metadata")
	}

	entry := requireSingleDebugEntry(t, observed)
	requireLogFieldString(t, entry, "instanceID", "ocid1.instance.oc1..override")
	requireLogFieldString(t, entry, "canonicalRegion", "us-chicago-1")
	requireLogFieldString(t, entry, "compartmentID", stubCompartmentID)

	warns := observed.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warns))
	}
}

func TestLogIMDSMetadataOfflineSkipsIMDS(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	client := newOfflineStubIMDS()
	ctrl := &stubController{
		mode:        modeEnforce,
		runErr:      nil,
		runCalled:   false,
		deadline:    time.Time{},
		deadlineSet: false,
	}

	logIMDSMetadata(
		context.Background(),
		logger,
		client,
		ctrl,
		"  ocid1.instance.oc1..offline  ",
		true,
	)

	assertNoIMDSCalls(t, client)
	assertOfflineLog(t, observed, "ocid1.instance.oc1..offline")
}

func TestMainIntegratesDefaultDependencies(t *testing.T) {
	server := newIPv4TestServer(
		t,
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			if req.Header.Get(imdsAuthHeaderKey) != imdsAuthHeaderVal {
				t.Fatalf(
					"expected IMDS authorization header %q, got %q",
					imdsAuthHeaderVal,
					req.Header.Get(imdsAuthHeaderKey),
				)
			}

			switch req.URL.Path {
			case "/opc/v2/instance/region":
				_, _ = writer.Write([]byte("us-denver-1"))
			case "/opc/v2/instance/regionInfo":
				_, _ = writer.Write([]byte(`{"canonicalRegionName":"us-denver-1"}`))
			case "/opc/v2/instance/id":
				_, _ = writer.Write([]byte("ocid1.instance.oc1..main"))
			case "/opc/v2/instance/compartmentId":
				_, _ = writer.Write([]byte("ocid1.compartment.oc1..main"))
			case "/opc/v2/instance/shape-config":
				_, _ = writer.Write([]byte(`{"ocpus":1,"memoryInGBs":1}`))
			default:
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
		}),
	)
	t.Cleanup(server.Close)

	t.Setenv(imdsEndpointEnv, server.URL+"/opc/v2")

	originalArgs := os.Args
	os.Args = []string{
		"shaper",
		"--mode",
		"noop",
		"--log-level",
		"error",
		"--config",
		"./testdata/config.yaml",
	}

	defer func() { os.Args = originalArgs }()

	done := make(chan struct{})

	go func() {
		defer close(done)

		main()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("main did not return in time")
	}
}

func assertInfoLogEntry(
	t *testing.T,
	entries []observer.LoggedEntry,
	version, commit, date string,
) {
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

func requireRunInvoked(t *testing.T, ctrl *stubController) {
	t.Helper()

	if ctrl == nil || !ctrl.runCalled {
		t.Fatal("expected controller Run to be invoked")
	}
}

func requireDeadlineCaptured(t *testing.T, ctrl *stubController) {
	t.Helper()

	if ctrl == nil {
		t.Fatal("controller stub is nil")
	}

	if !ctrl.deadlineSet {
		t.Fatal("expected controller Run to capture deadline")
	}

	if ctrl.deadline.IsZero() {
		t.Fatal("expected controller Run deadline to be set")
	}
}

func requireShutdownDuration(
	t *testing.T,
	entries []observer.LoggedEntry,
	expected time.Duration,
) {
	t.Helper()

	if len(entries) == 0 {
		t.Fatalf("expected startup log entry, got %+v", entries)
	}

	duration, ok := fieldDuration(entries[0].Context, "shutdownAfter")
	if !ok || duration != expected {
		t.Fatalf("expected shutdownAfter duration %v, got %v (present=%v)", expected, duration, ok)
	}
}

func runShutdownScenario(t *testing.T, runErr error, reason string) {
	t.Helper()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	ctrl := new(stubController)
	ctrl.runErr = runErr

	deps := defaultRunDeps()
	deps.currentBuildInfo = func() buildinfo.Info {
		return stubBuildInfo("test-version", "", "")
	}
	deps.newLogger = func(string) (*zap.Logger, error) {
		return logger, nil
	}
	deps.loadConfig = loadConfigStub()
	deps.newController = func(context.Context, string, runtimeConfig, imds.Client) (adapt.Controller, poolStarter, error) {
		return ctrl, nil, nil
	}

	exitCode := run(t.Context(), []string{"--shutdown-after", "50ms"}, deps, io.Discard)
	if exitCode != exitCodeSuccess {
		t.Fatalf("expected zero exit code, got %d", exitCode)
	}

	requireRunInvoked(t, ctrl)

	stoppedEntries := observed.FilterMessage("controller stopped").All()
	if len(stoppedEntries) != 1 {
		t.Fatalf("expected controller stopped log entry, got %+v", observed.All())
	}

	if got := fieldString(stoppedEntries[0].Context, "reason"); got != reason {
		t.Fatalf("expected reason %q, got %q", reason, got)
	}

	if failureEntries := observed.FilterMessage("controller execution failed").All(); len(
		failureEntries,
	) != 0 {
		t.Fatalf("expected no failure logs, got %+v", failureEntries)
	}
}

type stubController struct {
	mode        string
	runErr      error
	runCalled   bool
	deadline    time.Time
	deadlineSet bool
}

func (c *stubController) Run(ctx context.Context) error {
	c.runCalled = true

	if deadline, ok := ctx.Deadline(); ok {
		c.deadline = deadline
		c.deadlineSet = true
	} else {
		c.deadline = time.Time{}
		c.deadlineSet = false
	}

	return c.runErr
}

func (c *stubController) Mode() string {
	return c.mode
}

func fieldString(fields []zap.Field, key string) string {
	for _, field := range fields {
		if field.Key == key {
			return field.String
		}
	}

	return ""
}

func fieldBool(fields []zap.Field, key string) (bool, bool) {
	for _, field := range fields {
		if field.Key != key {
			continue
		}

		if field.Type == zapcore.BoolType {
			return field.Integer != 0, true
		}

		return false, true
	}

	return false, false
}

func fieldFloat(fields []zap.Field, key string) float64 {
	for _, field := range fields {
		if field.Key != key {
			continue
		}

		if field.Type == zapcore.Float64Type {
			if field.Integer < 0 {
				return 0
			}

			return math.Float64frombits(uint64(field.Integer))
		}

		if field.Type == zapcore.Float32Type {
			if field.Integer < 0 || field.Integer > int64(maxUint32) {
				return 0
			}

			return float64(math.Float32frombits(uint32(field.Integer)))
		}
	}

	return 0
}

func requireLogFieldString(t *testing.T, entry observer.LoggedEntry, key, want string) {
	t.Helper()

	if got := fieldString(entry.Context, key); got != want {
		t.Fatalf("expected %s field %q, got %+v", key, want, entry.Context)
	}
}

func requireLogFieldFloat(t *testing.T, entry observer.LoggedEntry, key string, want float64) {
	t.Helper()

	if got := fieldFloat(entry.Context, key); got != want {
		t.Fatalf("expected %s field %v, got %+v", key, want, entry.Context)
	}
}

func requireSingleDebugEntry(t *testing.T, observed *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()

	entries := observed.FilterLevelExact(zapcore.DebugLevel).All()
	if len(entries) == 0 {
		t.Fatalf("expected debug log entry, got %+v", observed.All())
	}

	return entries[0]
}

func fieldDuration(fields []zap.Field, key string) (time.Duration, bool) {
	for _, field := range fields {
		if field.Key != key {
			continue
		}

		if field.Type == zapcore.DurationType {
			return time.Duration(field.Integer), true
		}

		return 0, true
	}

	return 0, false
}

func stubBuildInfo(version, commit, date string) buildinfo.Info {
	return buildinfo.Info{
		Version:   version,
		GitCommit: commit,
		BuildDate: date,
	}
}

func loadConfigStub() func(string) (runtimeConfig, error) {
	return func(string) (runtimeConfig, error) {
		cfg := defaultRuntimeConfig()
		cfg.OCI.CompartmentID = stubCompartmentID

		return cfg, nil
	}
}

// newIPv4TestServer binds to the IPv4 loopback explicitly so tests still work when
// the sandbox forbids listening on IPv6.
func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	server := httptest.NewUnstartedServer(handler)

	var lc net.ListenConfig

	listener, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp4: %v", err)
	}

	server.Listener = listener
	server.Start()

	return server
}

type stubPoolStarter struct {
	startCount int
}

func (s *stubPoolStarter) Start(context.Context) {
	s.startCount++
}

type stubMetricsAdapter struct{}

func newStubMetricsClient() *stubMetricsAdapter {
	return &stubMetricsAdapter{}
}

func (s *stubMetricsAdapter) QueryP95CPU(context.Context, string) (float64, error) {
	return 0.25, nil
}

type stubIMDSClient struct {
	region               string
	regionErr            error
	canonicalRegion      string
	canonicalRegionErr   error
	instanceID           string
	instanceErr          error
	compartmentID        string
	compartmentErr       error
	shape                imds.ShapeConfig
	shapeErr             error
	regionCalls          int
	canonicalRegionCalls int
	instanceCalls        int
	compartmentCalls     int
	shapeCalls           int
}

func (s *stubIMDSClient) Region(context.Context) (string, error) {
	s.regionCalls++

	return s.region, s.regionErr
}

func (s *stubIMDSClient) CanonicalRegion(context.Context) (string, error) {
	s.canonicalRegionCalls++

	return s.canonicalRegion, s.canonicalRegionErr
}

func (s *stubIMDSClient) InstanceID(context.Context) (string, error) {
	s.instanceCalls++

	return s.instanceID, s.instanceErr
}

func (s *stubIMDSClient) CompartmentID(context.Context) (string, error) {
	s.compartmentCalls++

	return s.compartmentID, s.compartmentErr
}

func (s *stubIMDSClient) ShapeConfig(context.Context) (imds.ShapeConfig, error) {
	s.shapeCalls++

	return s.shape, s.shapeErr
}

func newOfflineStubIMDS() *stubIMDSClient {
	return &stubIMDSClient{
		region:             "",
		regionErr:          errRegionDown,
		canonicalRegion:    "",
		canonicalRegionErr: errRegionDown,
		instanceID:         "",
		instanceErr:        errInstanceDown,
		compartmentID:      "",
		compartmentErr:     errInstanceDown,
		shape: imds.ShapeConfig{
			OCPUs:                     0,
			MemoryInGBs:               0,
			BaselineOcpuUtilization:   "",
			BaselineOCPUs:             0,
			ThreadsPerCore:            0,
			NetworkingBandwidthInGbps: 0,
			MaxVnicAttachments:        0,
		},
		shapeErr:             errShapeDown,
		regionCalls:          0,
		canonicalRegionCalls: 0,
		instanceCalls:        0,
		compartmentCalls:     0,
		shapeCalls:           0,
	}
}

func newLoggingStubIMDS(
	region string,
	regionErr error,
	canonicalRegion string,
	canonicalErr error,
	instanceID string,
	instanceErr error,
	compartmentID string,
	compartmentErr error,
	shape imds.ShapeConfig,
	shapeErr error,
) *stubIMDSClient {
	return &stubIMDSClient{
		region:               region,
		regionErr:            regionErr,
		canonicalRegion:      canonicalRegion,
		canonicalRegionErr:   canonicalErr,
		instanceID:           instanceID,
		instanceErr:          instanceErr,
		compartmentID:        compartmentID,
		compartmentErr:       compartmentErr,
		shape:                shape,
		shapeErr:             shapeErr,
		regionCalls:          0,
		canonicalRegionCalls: 0,
		instanceCalls:        0,
		compartmentCalls:     0,
		shapeCalls:           0,
	}
}

func stubShapeConfig(ocpus, memory float64) imds.ShapeConfig {
	return imds.ShapeConfig{
		OCPUs:                     ocpus,
		MemoryInGBs:               memory,
		BaselineOcpuUtilization:   "",
		BaselineOCPUs:             0,
		ThreadsPerCore:            0,
		NetworkingBandwidthInGbps: 0,
		MaxVnicAttachments:        0,
	}
}

func assertNoIMDSCalls(t *testing.T, client *stubIMDSClient) {
	t.Helper()

	if client.regionCalls != 0 || client.canonicalRegionCalls != 0 || client.instanceCalls != 0 ||
		client.compartmentCalls != 0 || client.shapeCalls != 0 {
		t.Fatalf(
			"expected offline mode to skip imds lookups, got region=%d canonical=%d instance=%d compartment=%d shape=%d",
			client.regionCalls,
			client.canonicalRegionCalls,
			client.instanceCalls,
			client.compartmentCalls,
			client.shapeCalls,
		)
	}
}

func assertOfflineLog(t *testing.T, observed *observer.ObservedLogs, expectedID string) {
	t.Helper()

	if warns := observed.FilterLevelExact(zapcore.WarnLevel).All(); len(warns) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warns))
	}

	entries := observed.FilterLevelExact(zapcore.DebugLevel).All()
	if len(entries) != 1 {
		t.Fatalf("expected single debug entry, got %d", len(entries))
	}

	entry := entries[0]
	if got := fieldString(entry.Context, "instanceID"); got != expectedID {
		t.Fatalf("expected trimmed override instance id, got %q", got)
	}

	offline, ok := fieldBool(entry.Context, "offline")
	if !ok || !offline {
		t.Fatalf("expected offline field to be true, got %v (ok=%v)", offline, ok)
	}
}
