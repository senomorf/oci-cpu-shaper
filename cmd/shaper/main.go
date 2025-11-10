package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"

	"oci-cpu-shaper/internal/buildinfo"
	"oci-cpu-shaper/pkg/adapt"
	"oci-cpu-shaper/pkg/imds"
)

type options struct {
	configPath string
	logLevel   string
	mode       string
}

func main() {
	opts := parseFlags()

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

func parseFlags() options {
	var opts options

	flag.StringVar(&opts.configPath, "config", "/etc/oci-cpu-shaper/config.yaml", "Path to the shaper configuration file")
	flag.StringVar(&opts.logLevel, "log-level", "info", "Structured log level (debug, info, warn, error)")
	flag.StringVar(&opts.mode, "mode", "dry-run", "Controller mode to use (dry-run, enforce, noop)")

	flag.Parse()

	return opts
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
