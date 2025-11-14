//go:build e2e

package main

import (
	"context"
	"os"
	"sync/atomic"

	"go.uber.org/zap"
	"oci-cpu-shaper/internal/buildinfo"
	"oci-cpu-shaper/internal/e2eclient"
	"oci-cpu-shaper/pkg/adapt"
	metricshttp "oci-cpu-shaper/pkg/http/metrics"
	"oci-cpu-shaper/pkg/imds"
)

var e2eLogger atomic.Pointer[zap.Logger]

func defaultRunDeps() runDeps {
	deps := runDeps{
		newLogger: newLogger,
		newIMDS:   defaultIMDSFactory,
		newController: func(
			ctx context.Context,
			mode string,
			cfg runtimeConfig,
			imdsClient imds.Client,
			recorder adapt.MetricsRecorder,
		) (adapt.Controller, poolStarter, error) {
			logger := e2eLogger.Load()
			if logger != nil && recorder != nil {
				recorder = e2eclient.NewLoggingRecorder(logger, recorder)
			}

			return defaultControllerFactory(ctx, mode, cfg, imdsClient, recorder)
		},
		currentBuildInfo:   buildinfo.Current,
		loadConfig:         loadConfig,
		newMetricsExporter: metricshttp.NewExporter,
		startMetricsServer: startMetricsServer,
		versionWriter:      os.Stdout,
	}

	deps.newLogger = func(level string) (*zap.Logger, error) {
		logger, err := newLogger(level)
		if err == nil {
			e2eLogger.Store(logger)
		}

		return logger, err
	}

	return deps
}
