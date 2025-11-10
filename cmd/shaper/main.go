// Package main wires the shaper CLI entrypoint.
package main

//nolint:depguard // main wires project-internal modules and zap logging
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
	"oci-cpu-shaper/pkg/imds"
)

const (
	defaultConfigPath = "/etc/oci-cpu-shaper/config.yaml"
	defaultLogLevel   = "info"
	modeDryRun        = "dry-run"
	modeEnforce       = "enforce"
	modeNoop          = "noop"

	exitCodeSuccess      = 0
	exitCodeRuntimeError = 1
	exitCodeParseError   = 2
)

func main() {
	code := run(context.Background(), os.Args[1:], defaultRunDeps(), os.Stderr)
	if code != 0 {
		os.Exit(code)
	}
}

type runDeps struct {
	newLogger     func(level string) (*zap.Logger, error)
	newIMDS       func() imds.Client
	newController func(mode string) adapt.Controller
}

func defaultRunDeps() runDeps {
	return runDeps{
		newLogger:     newLogger,
		newIMDS:       defaultIMDSFactory,
		newController: defaultControllerFactory,
	}
}

func run(ctx context.Context, args []string, deps runDeps, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		_, ferr := fmt.Fprintf(stderr, "%v\n", err)
		if ferr != nil {
			return exitCodeParseError
		}

		return exitCodeParseError
	}

	logger, err := deps.newLogger(opts.logLevel)
	if err != nil {
		_, ferr := fmt.Fprintf(stderr, "failed to configure logger: %v\n", err)
		if ferr != nil {
			return exitCodeRuntimeError
		}

		return exitCodeRuntimeError
	}

	defer func() {
		_ = logger.Sync()
	}()

	info := buildinfo.Current()
	logger.Info(
		"starting oci-cpu-shaper",
		zap.String("version", info.Version),
		zap.String("commit", info.GitCommit),
		zap.String("buildDate", info.BuildDate),
		zap.String("configPath", opts.configPath),
		zap.String("mode", opts.mode),
	)

	imdsClient := deps.newIMDS()
	controller := deps.newController(opts.mode)

	region, _ := imdsClient.Region(ctx)
	instanceID, _ := imdsClient.InstanceID(ctx)
	logger.Debug("initialized subsystems",
		zap.String("dummyRegion", region),
		zap.String("instanceID", instanceID),
		zap.String("controllerMode", controller.Mode()),
	)

	runErr := controller.Run(ctx)
	if runErr != nil {
		logger.Error("controller execution failed", zap.Error(runErr))

		return exitCodeRuntimeError
	}

	return exitCodeSuccess
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
	mode string,
) adapt.Controller {
	return adapt.NewNoopController(mode)
}

//nolint:ireturn // returns interface to support substitutable IMDS clients
func defaultIMDSFactory() imds.Client {
	return imds.NewDummyClient()
}

func isValidMode(mode string) bool {
	switch mode {
	case modeDryRun, modeEnforce, modeNoop:
		return true
	default:
		return false
	}
}
