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
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	logger, err := newLogger(opts.logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to configure logger: %v\n", err)
		os.Exit(1)
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

	ctx := context.Background()

	imdsClient := imds.NewDummyClient()
	controller := adapt.NewNoopController(opts.mode)

	region, _ := imdsClient.Region(ctx)
	instanceID, _ := imdsClient.InstanceID(ctx)
	logger.Debug("initialized subsystems",
		zap.String("dummyRegion", region),
		zap.String("instanceID", instanceID),
		zap.String("controllerMode", controller.Mode()),
	)

	if err := controller.Run(ctx); err != nil {
		logger.Fatal("controller execution failed", zap.Error(err))
	}
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
