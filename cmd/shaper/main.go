// Package main wires the shaper CLI entrypoint.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"go.uber.org/zap"
	"oci-cpu-shaper/internal/buildinfo"
	"oci-cpu-shaper/pkg/adapt"
	"oci-cpu-shaper/pkg/est"
	"oci-cpu-shaper/pkg/imds"
	"oci-cpu-shaper/pkg/oci"
	"oci-cpu-shaper/pkg/shape"
)

const (
	defaultConfigPath = "/etc/oci-cpu-shaper/config.yaml"
	defaultLogLevel   = "info"
	modeDryRun        = "dry-run"
	modeEnforce       = "enforce"
	modeNoop          = "noop"

	imdsEndpointEnv = "OCI_CPU_SHAPER_IMDS_ENDPOINT"

	offlineInstanceFallback = "offline-instance"

	exitCodeSuccess      = 0
	exitCodeRuntimeError = 1
	exitCodeParseError   = 2
)

func main() {
	code := run(context.Background(), os.Args[1:], defaultRunDeps(), os.Stderr)
	if code != 0 {
		exitProcess(code)
	}
}

var exitProcess = os.Exit //nolint:gochecknoglobals // replaceable for tests

type runDeps struct {
	newLogger     func(level string) (*zap.Logger, error)
	newIMDS       func() imds.Client
	newController func(
		ctx context.Context,
		mode string,
		cfg runtimeConfig,
		imdsClient imds.Client,
	) (adapt.Controller, poolStarter, error)
	currentBuildInfo func() buildinfo.Info
	loadConfig       func(path string) (runtimeConfig, error)
}

type poolStarter interface {
	Start(ctx context.Context)
}

type metricsClientFactory func(compartmentID string) (oci.MetricsClient, error)

type metricsClientFactoryKey struct{}

func withMetricsClientFactory(ctx context.Context, factory metricsClientFactory) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if factory == nil {
		return ctx
	}

	return context.WithValue(ctx, metricsClientFactoryKey{}, factory)
}

func metricsClientFactoryFromContext(ctx context.Context) metricsClientFactory {
	if ctx != nil {
		if factory, ok := ctx.Value(metricsClientFactoryKey{}).(metricsClientFactory); ok &&
			factory != nil {
			return factory
		}
	}

	return buildInstancePrincipalMetricsClient
}

var (
	errControllerIMDSRequired        = errors.New("controller factory: imds client is required")
	errControllerCompartmentRequired = errors.New(
		"controller factory: OCI compartment ID is required",
	)
	errMetricsDelegateNil = errors.New("metrics client: nil delegate")
)

func defaultRunDeps() runDeps {
	return runDeps{
		newLogger:        newLogger,
		newIMDS:          defaultIMDSFactory,
		newController:    defaultControllerFactory,
		currentBuildInfo: buildinfo.Current,
		loadConfig:       loadConfig,
	}
}

func run(ctx context.Context, args []string, deps runDeps, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		return writeError(stderr, err, exitCodeParseError)
	}

	cfg, err := deps.loadConfig(opts.configPath)
	if err != nil {
		return writeError(
			stderr,
			fmt.Errorf("failed to load configuration: %w", err),
			exitCodeRuntimeError,
		)
	}

	logger, err := deps.newLogger(opts.logLevel)
	if err != nil {
		return writeError(
			stderr,
			fmt.Errorf("failed to configure logger: %w", err),
			exitCodeRuntimeError,
		)
	}

	defer func() {
		_ = logger.Sync()
	}()

	info := deps.currentBuildInfo()
	logger.Info(
		"starting oci-cpu-shaper",
		zap.String("version", info.Version),
		zap.String("commit", info.GitCommit),
		zap.String("buildDate", info.BuildDate),
		zap.String("configPath", opts.configPath),
		zap.String("mode", opts.mode),
	)

	imdsClient := deps.newIMDS()

	controller, pool, buildErr := deps.newController(ctx, opts.mode, cfg, imdsClient)
	if buildErr != nil {
		logger.Error("failed to build controller", zap.Error(buildErr))

		return exitCodeRuntimeError
	}

	if pool != nil {
		pool.Start(ctx)
	}

	logIMDSMetadata(ctx, logger, imdsClient, controller, cfg.OCI.InstanceID)

	runErr := controller.Run(ctx)
	if runErr != nil {
		logger.Error("controller execution failed", zap.Error(runErr))

		return exitCodeRuntimeError
	}

	return exitCodeSuccess
}

func writeError(dst io.Writer, err error, code int) int {
	if err == nil {
		return code
	}

	_, ferr := fmt.Fprintf(dst, "%v\n", err)
	if ferr != nil {
		return code
	}

	return code
}

func newLogger(level string) (*zap.Logger, error) {
	if level == "" {
		level = defaultLogLevel
	}

	cfg := zap.NewProductionConfig()

	err := cfg.Level.UnmarshalText([]byte(level))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidLogLevel, err)
	}

	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.MessageKey = "message"
	cfg.EncoderConfig.LevelKey = "level"
	cfg.EncoderConfig.CallerKey = "caller"

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build zap logger: %w", err)
	}

	return logger, nil
}

type options struct {
	configPath string
	logLevel   string
	mode       string
}

func parseArgs(args []string) (options, error) {
	var opts options

	flagSet := flag.NewFlagSet("shaper", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	flagSet.StringVar(
		&opts.configPath,
		"config",
		defaultConfigPath,
		"Path to the shaper configuration file",
	)
	flagSet.StringVar(
		&opts.logLevel,
		"log-level",
		defaultLogLevel,
		"Structured log level (debug, info, warn, error)",
	)
	flagSet.StringVar(
		&opts.mode,
		"mode",
		modeDryRun,
		"Controller mode to use (dry-run, enforce, noop)",
	)

	err := flagSet.Parse(args)
	if err != nil {
		return options{}, fmt.Errorf("parse CLI arguments: %w", err)
	}

	opts.mode = strings.ToLower(strings.TrimSpace(opts.mode))
	if opts.mode == "" {
		opts.mode = modeDryRun
	}

	if !isValidMode(opts.mode) {
		return options{}, fmt.Errorf(
			"%w: %q (supported: %s, %s, %s)",
			errUnsupportedMode,
			opts.mode,
			modeDryRun,
			modeEnforce,
			modeNoop,
		)
	}

	opts.logLevel = strings.TrimSpace(opts.logLevel)
	if opts.logLevel == "" {
		opts.logLevel = defaultLogLevel
	}

	opts.configPath = strings.TrimSpace(opts.configPath)
	if opts.configPath == "" {
		opts.configPath = defaultConfigPath
	}

	return opts, nil
}

var (
	errInvalidLogLevel = errors.New("invalid log level")
	errUnsupportedMode = errors.New("unsupported mode provided")
)

//nolint:ireturn // factory intentionally hides controller implementation
func defaultControllerFactory(
	ctx context.Context,
	mode string,
	cfg runtimeConfig,
	imdsClient imds.Client,
) (adapt.Controller, poolStarter, error) {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		trimmed = modeDryRun
	}

	if trimmed == modeNoop {
		return adapt.NewNoopController(trimmed), nil, nil
	}

	if imdsClient == nil {
		return nil, nil, errControllerIMDSRequired
	}

	return buildAdaptiveController(ctx, trimmed, cfg, imdsClient)
}

//nolint:ireturn // helper returns controller interface for wiring
func buildAdaptiveController(
	ctx context.Context,
	mode string,
	cfg runtimeConfig,
	imdsClient imds.Client,
) (adapt.Controller, poolStarter, error) {
	offline := cfg.OCI.Offline

	instanceID, err := resolveInstanceID(ctx, cfg, offline, imdsClient)
	if err != nil {
		return nil, nil, err
	}

	compartmentID := strings.TrimSpace(cfg.OCI.CompartmentID)
	if compartmentID == "" && !offline {
		return nil, nil, errControllerCompartmentRequired
	}

	metricsClient, err := createMetricsClient(ctx, cfg, offline, compartmentID)
	if err != nil {
		return nil, nil, err
	}

	pool, err := shape.NewPool(cfg.Pool.Workers, cfg.Pool.Quantum)
	if err != nil {
		return nil, nil, fmt.Errorf("build worker pool: %w", err)
	}

	sampler := est.NewSampler(nil, cfg.Estimator.Interval)

	controllerCfg := adapt.Config{
		ResourceID:       instanceID,
		Mode:             mode,
		TargetStart:      cfg.Controller.TargetStart,
		TargetMin:        cfg.Controller.TargetMin,
		TargetMax:        cfg.Controller.TargetMax,
		StepUp:           cfg.Controller.StepUp,
		StepDown:         cfg.Controller.StepDown,
		FallbackTarget:   cfg.Controller.FallbackTarget,
		GoalLow:          cfg.Controller.GoalLow,
		GoalHigh:         cfg.Controller.GoalHigh,
		Interval:         cfg.Controller.Interval,
		RelaxedInterval:  cfg.Controller.RelaxedInterval,
		RelaxedThreshold: cfg.Controller.RelaxedThreshold,
	}

	controller, err := adapt.NewAdaptiveController(controllerCfg, metricsClient, sampler, pool)
	if err != nil {
		return nil, nil, fmt.Errorf("build adaptive controller: %w", err)
	}

	return controller, pool, nil
}

func resolveInstanceID(
	ctx context.Context,
	cfg runtimeConfig,
	offline bool,
	imdsClient imds.Client,
) (string, error) {
	instanceID := strings.TrimSpace(cfg.OCI.InstanceID)
	if instanceID != "" {
		return instanceID, nil
	}

	if offline {
		return offlineInstanceFallback, nil
	}

	fetchedID, err := imdsClient.InstanceID(ctx)
	if err != nil {
		return "", fmt.Errorf("lookup instance ocid: %w", err)
	}

	return strings.TrimSpace(fetchedID), nil
}

//nolint:ireturn // wiring requires interface for factories.
func createMetricsClient(
	ctx context.Context,
	cfg runtimeConfig,
	offline bool,
	compartmentID string,
) (oci.MetricsClient, error) {
	if offline {
		return oci.NewStaticMetricsClient(cfg.Controller.TargetStart), nil
	}

	factory := metricsClientFactoryFromContext(ctx)

	metricsClient, err := factory(compartmentID)
	if err != nil {
		return nil, fmt.Errorf("build monitoring client: %w", err)
	}

	return metricsClient, nil
}

//nolint:ireturn // factory returns interface for dependency substitution.
func buildInstancePrincipalMetricsClient(compartmentID string) (oci.MetricsClient, error) {
	client, err := oci.NewInstancePrincipalClient(compartmentID)
	if err != nil {
		return nil, fmt.Errorf("new instance principal client: %w", err)
	}

	return &instancePrincipalMetricsClient{client: client}, nil
}

type instancePrincipalMetricsClient struct {
	client *oci.Client
}

func (m *instancePrincipalMetricsClient) QueryP95CPU(
	ctx context.Context,
	resourceID string,
) (float64, error) {
	if m == nil || m.client == nil {
		return 0, errMetricsDelegateNil
	}

	value, err := m.client.QueryP95CPU(ctx, resourceID, true)
	if err != nil {
		return 0, fmt.Errorf("query p95 cpu: %w", err)
	}

	return float64(value), nil
}

//nolint:ireturn // returns interface to support substitutable IMDS clients
func defaultIMDSFactory() imds.Client {
	endpoint := strings.TrimSpace(os.Getenv(imdsEndpointEnv))

	var opts []imds.Option
	if endpoint != "" {
		opts = append(opts, imds.WithBaseURL(endpoint))
	}

	return imds.NewClient(nil, opts...)
}

func logIMDSMetadata(
	ctx context.Context,
	logger *zap.Logger,
	client imds.Client,
	controller adapt.Controller,
	overrideInstanceID string,
) {
	region, regionErr := client.Region(ctx)
	if regionErr != nil {
		logger.Warn("failed to query instance region", zap.Error(regionErr))
	}

	instanceID := strings.TrimSpace(overrideInstanceID)

	var instanceErr error
	if instanceID == "" {
		instanceID, instanceErr = client.InstanceID(ctx)
		if instanceErr != nil {
			logger.Warn("failed to query instance OCID", zap.Error(instanceErr))
		}
	}

	shapeCfg, shapeErr := client.ShapeConfig(ctx)
	if shapeErr != nil {
		logger.Warn("failed to query instance shape config", zap.Error(shapeErr))
	}

	fields := []zap.Field{
		zap.String("controllerMode", controller.Mode()),
	}

	if regionErr == nil {
		fields = append(fields, zap.String("region", region))
	}

	if instanceErr == nil {
		fields = append(fields, zap.String("instanceID", instanceID))
	}

	if shapeErr == nil {
		fields = append(fields,
			zap.Float64("shapeOCPUs", shapeCfg.OCPUs),
			zap.Float64("shapeMemoryGB", shapeCfg.MemoryInGBs),
		)
	}

	logger.Debug("initialized subsystems", fields...)
}

func isValidMode(mode string) bool {
	switch mode {
	case modeDryRun, modeEnforce, modeNoop:
		return true
	default:
		return false
	}
}
