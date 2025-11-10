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
	"oci-cpu-shaper/pkg/imds"
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
		newLogger: newLogger,
		newIMDS:   imds.NewDummyClient,
		newController: func(mode string) adapt.Controller {
			return adapt.NewNoopController(mode)
		},
	}
}

func run(ctx context.Context, args []string, deps runDeps, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		if _, ferr := fmt.Fprintf(stderr, "%v\n", err); ferr != nil {
			return 2
		}
		return 2
	}

	logger, err := deps.newLogger(opts.logLevel)
	if err != nil {
		if _, ferr := fmt.Fprintf(stderr, "failed to configure logger: %v\n", err); ferr != nil {
			return 1
		}
		return 1
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

	if err := controller.Run(ctx); err != nil {
		logger.Error("controller execution failed", zap.Error(err))
		return 1
	}

	return 0
}

func newLogger(level string) (*zap.Logger, error) {
	if level == "" {
		level = "info"
	}

	cfg := zap.NewProductionConfig()
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		return nil, errors.Join(errors.New("invalid log level"), err)
	}
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.MessageKey = "message"
	cfg.EncoderConfig.LevelKey = "level"
	cfg.EncoderConfig.CallerKey = "caller"

	return cfg.Build()
}

type options struct {
	configPath string
	logLevel   string
	mode       string
}

var validModes = map[string]struct{}{
	"dry-run": {},
	"enforce": {},
	"noop":    {},
}

func parseArgs(args []string) (options, error) {
	var opts options

	fs := flag.NewFlagSet("shaper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configPath, "config", "/etc/oci-cpu-shaper/config.yaml", "Path to the shaper configuration file")
	fs.StringVar(&opts.logLevel, "log-level", "info", "Structured log level (debug, info, warn, error)")
	fs.StringVar(&opts.mode, "mode", "dry-run", "Controller mode to use (dry-run, enforce, noop)")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	opts.mode = strings.ToLower(strings.TrimSpace(opts.mode))
	if opts.mode == "" {
		opts.mode = "dry-run"
	}
	if _, ok := validModes[opts.mode]; !ok {
		return options{}, fmt.Errorf("unsupported mode %q (supported: dry-run, enforce, noop)", opts.mode)
	}

	opts.logLevel = strings.TrimSpace(opts.logLevel)
	if opts.logLevel == "" {
		opts.logLevel = "info"
	}

	opts.configPath = strings.TrimSpace(opts.configPath)
	if opts.configPath == "" {
		opts.configPath = "/etc/oci-cpu-shaper/config.yaml"
	}

	return opts, nil
}
