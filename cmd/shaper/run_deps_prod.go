//go:build !e2e

package main

import (
	"os"

	"oci-cpu-shaper/internal/buildinfo"
	metricshttp "oci-cpu-shaper/pkg/http/metrics"
)

func defaultRunDeps() runDeps {
	return runDeps{
		newLogger:          newLogger,
		newIMDS:            defaultIMDSFactory,
		newController:      defaultControllerFactory,
		currentBuildInfo:   buildinfo.Current,
		loadConfig:         loadConfig,
		newMetricsExporter: metricshttp.NewExporter,
		startMetricsServer: startMetricsServer,
		versionWriter:      os.Stdout,
	}
}
