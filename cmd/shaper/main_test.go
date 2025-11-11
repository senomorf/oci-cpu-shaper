package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"math"
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

	deps.loadConfig = loadConfigStub(stubCompartmentID)

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

	deps.loadConfig = loadConfigStub(stubCompartmentID)

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

	originalFactory := newMetricsClient

	t.Cleanup(func() { newMetricsClient = originalFactory })

	fakeMetrics := newStubMetricsClient()
	newMetricsClient = func(string) (oci.MetricsClient, error) {
		return fakeMetrics, nil
	}

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = "ocid1.compartment.oc1..controller"
	cfg.Pool.Workers = 1
	cfg.Estimator.Interval = 500 * time.Millisecond

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceID = "ocid1.instance.oc1..controller"

	controller, pool, err := defaultControllerFactory(
		context.Background(),
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

	originalFactory := newMetricsClient

	t.Cleanup(func() { newMetricsClient = originalFactory })

	newMetricsClient = func(string) (oci.MetricsClient, error) {
		return nil, errStubControllerRun
	}

	cfg := defaultRuntimeConfig()
	cfg.OCI.CompartmentID = "ocid1.compartment.oc1..metrics"

	imdsClient := new(stubIMDSClient)
	imdsClient.instanceID = "ocid1.instance.oc1..metrics"

	_, _, err := defaultControllerFactory(
		context.Background(),
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

	server := httptest.NewServer(
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

	client := &stubIMDSClient{
		region:      "us-ashburn-1",
		regionErr:   nil,
		instanceID:  "ocid1.instance.oc1..exampleuniqueID",
		instanceErr: nil,
		shape: imds.ShapeConfig{
			OCPUs:                     4,
			MemoryInGBs:               64,
			BaselineOcpuUtilization:   "",
			BaselineOCPUs:             0,
			ThreadsPerCore:            0,
			NetworkingBandwidthInGbps: 0,
			MaxVnicAttachments:        0,
		},
		shapeErr: nil,
	}

	ctrl := &stubController{mode: modeDryRun, runErr: nil, runCalled: false}

	logIMDSMetadata(context.Background(), logger, client, ctrl)

	entries := observed.FilterLevelExact(zapcore.DebugLevel).All()
	if len(entries) == 0 {
		t.Fatalf("expected debug log entry, got %+v", observed.All())
	}

	entry := entries[0]
	if fieldString(entry.Context, "region") != "us-ashburn-1" {
		t.Fatalf("expected region field, got %+v", entry.Context)
	}

	if fieldString(entry.Context, "instanceID") != "ocid1.instance.oc1..exampleuniqueID" {
		t.Fatalf("expected instanceID field, got %+v", entry.Context)
	}

	if fieldFloat(entry.Context, "shapeOCPUs") != 4 {
		t.Fatalf("expected shapeOCPUs field, got %+v", entry.Context)
	}

	if fieldFloat(entry.Context, "shapeMemoryGB") != 64 {
		t.Fatalf("expected shapeMemoryGB field, got %+v", entry.Context)
	}
}

func TestLogIMDSMetadataWarnsOnFailures(t *testing.T) {
	t.Parallel()

	core, observed := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	client := &stubIMDSClient{
		region:      "",
		regionErr:   errRegionDown,
		instanceID:  "",
		instanceErr: errInstanceDown,
		shape: imds.ShapeConfig{
			OCPUs:                     0,
			MemoryInGBs:               0,
			BaselineOcpuUtilization:   "",
			BaselineOCPUs:             0,
			ThreadsPerCore:            0,
			NetworkingBandwidthInGbps: 0,
			MaxVnicAttachments:        0,
		},
		shapeErr: errShapeDown,
	}

	ctrl := &stubController{mode: modeNoop, runErr: nil, runCalled: false}

	logIMDSMetadata(context.Background(), logger, client, ctrl)

	warns := observed.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != 3 {
		t.Fatalf("expected three warnings, got %d", len(warns))
	}
}

func TestMainIntegratesDefaultDependencies(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/opc/v2/instance/region":
				_, _ = writer.Write([]byte("us-denver-1"))
			case "/opc/v2/instance/id":
				_, _ = writer.Write([]byte("ocid1.instance.oc1..main"))
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
	for _, field := range fields {
		if field.Key == key {
			return field.String
		}
	}

	return ""
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

		return 0
	}

	return 0
}

func stubBuildInfo(version, commit, date string) buildinfo.Info {
	return buildinfo.Info{
		Version:   version,
		GitCommit: commit,
		BuildDate: date,
	}
}

func loadConfigStub(compartmentID string) func(string) (runtimeConfig, error) {
	return func(string) (runtimeConfig, error) {
		cfg := defaultRuntimeConfig()
		cfg.OCI.CompartmentID = compartmentID

		return cfg, nil
	}
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
	region      string
	regionErr   error
	instanceID  string
	instanceErr error
	shape       imds.ShapeConfig
	shapeErr    error
}

func (s *stubIMDSClient) Region(context.Context) (string, error) {
	return s.region, s.regionErr
}

func (s *stubIMDSClient) InstanceID(context.Context) (string, error) {
	return s.instanceID, s.instanceErr
}

func (s *stubIMDSClient) ShapeConfig(context.Context) (imds.ShapeConfig, error) {
	return s.shape, s.shapeErr
}
