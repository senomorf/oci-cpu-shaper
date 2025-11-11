package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"oci-cpu-shaper/pkg/adapt"
	"oci-cpu-shaper/pkg/shape"
)

const (
	envTargetStart      = "SHAPER_TARGET_START"
	envTargetMin        = "SHAPER_TARGET_MIN"
	envTargetMax        = "SHAPER_TARGET_MAX"
	envStepUp           = "SHAPER_STEP_UP"
	envStepDown         = "SHAPER_STEP_DOWN"
	envSlowInterval     = "SHAPER_SLOW_INTERVAL"
	envRelaxedInterval  = "SHAPER_SLOW_INTERVAL_RELAXED"
	envFastInterval     = "SHAPER_FAST_INTERVAL"
	envPoolWorkers      = "SHAPER_WORKER_COUNT"
	envHTTPBind         = "HTTP_ADDR"
	envCompartmentID    = "OCI_COMPARTMENT_ID"
	envFallbackTarget   = "SHAPER_FALLBACK_TARGET"
	envRelaxedThreshold = "SHAPER_RELAXED_THRESHOLD"
	envGoalLow          = "SHAPER_GOAL_LOW"
	envGoalHigh         = "SHAPER_GOAL_HIGH"
)

type runtimeConfig struct {
	Controller controllerConfig
	Estimator  estimatorConfig
	Pool       poolConfig
	HTTP       httpConfig
	OCI        ociConfig
}

type controllerConfig struct {
	TargetStart      float64
	TargetMin        float64
	TargetMax        float64
	StepUp           float64
	StepDown         float64
	FallbackTarget   float64
	GoalLow          float64
	GoalHigh         float64
	Interval         time.Duration
	RelaxedInterval  time.Duration
	RelaxedThreshold float64
}

type estimatorConfig struct {
	Interval time.Duration
}

type poolConfig struct {
	Workers int
	Quantum time.Duration
}

type httpConfig struct {
	Bind string
}

type ociConfig struct {
	CompartmentID string
}

type fileConfig struct {
	Controller controllerFileConfig `yaml:"controller"`
	Estimator  estimatorFileConfig  `yaml:"estimator"`
	Pool       poolFileConfig       `yaml:"pool"`
	HTTP       httpFileConfig       `yaml:"http"`
	OCI        ociFileConfig        `yaml:"oci"`
}

type controllerFileConfig struct {
	TargetStart      *float64       `yaml:"targetStart"`
	TargetMin        *float64       `yaml:"targetMin"`
	TargetMax        *float64       `yaml:"targetMax"`
	StepUp           *float64       `yaml:"stepUp"`
	StepDown         *float64       `yaml:"stepDown"`
	FallbackTarget   *float64       `yaml:"fallbackTarget"`
	GoalLow          *float64       `yaml:"goalLow"`
	GoalHigh         *float64       `yaml:"goalHigh"`
	Interval         *time.Duration `yaml:"interval"`
	RelaxedInterval  *time.Duration `yaml:"relaxedInterval"`
	RelaxedThreshold *float64       `yaml:"relaxedThreshold"`
}

type estimatorFileConfig struct {
	Interval *time.Duration `yaml:"interval"`
}

type poolFileConfig struct {
	Workers *int           `yaml:"workers"`
	Quantum *time.Duration `yaml:"quantum"`
}

type httpFileConfig struct {
	Bind *string `yaml:"bind"`
}

type ociFileConfig struct {
	CompartmentID *string `yaml:"compartmentId"`
}

func defaultRuntimeConfig() runtimeConfig {
	defaults := adapt.DefaultConfig()

	var cfg runtimeConfig

	cfg.Controller.TargetStart = defaults.TargetStart
	cfg.Controller.TargetMin = defaults.TargetMin
	cfg.Controller.TargetMax = defaults.TargetMax
	cfg.Controller.StepUp = defaults.StepUp
	cfg.Controller.StepDown = defaults.StepDown
	cfg.Controller.FallbackTarget = defaults.FallbackTarget
	cfg.Controller.GoalLow = defaults.GoalLow
	cfg.Controller.GoalHigh = defaults.GoalHigh
	cfg.Controller.Interval = defaults.Interval
	cfg.Controller.RelaxedInterval = defaults.RelaxedInterval
	cfg.Controller.RelaxedThreshold = defaults.RelaxedThreshold

	cfg.Estimator.Interval = time.Second

	cfg.Pool.Workers = runtime.NumCPU()
	if cfg.Pool.Workers <= 0 {
		cfg.Pool.Workers = 1
	}

	cfg.Pool.Quantum = shape.DefaultQuantum

	cfg.HTTP.Bind = ":9108"

	return cfg
}

func loadConfig(path string) (runtimeConfig, error) {
	cfg := defaultRuntimeConfig()

	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		applyEnvOverrides(&cfg)

		return cfg, nil
	}

	data, err := os.ReadFile(trimmed)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return runtimeConfig{}, fmt.Errorf("read config file %q: %w", trimmed, err)
		}
	} else {
		var fileCfg fileConfig

		err := yaml.Unmarshal(data, &fileCfg)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("decode config file %q: %w", trimmed, err)
		}

		mergeControllerConfig(&cfg.Controller, fileCfg.Controller)
		mergeEstimatorConfig(&cfg.Estimator, fileCfg.Estimator)
		mergePoolConfig(&cfg.Pool, fileCfg.Pool)
		mergeHTTPConfig(&cfg.HTTP, fileCfg.HTTP)
		mergeOCIConfig(&cfg.OCI, fileCfg.OCI)
	}

	applyEnvOverrides(&cfg)

	return cfg, nil
}

func mergeControllerConfig(dst *controllerConfig, src controllerFileConfig) {
	assignFloat(&dst.TargetStart, src.TargetStart)
	assignFloat(&dst.TargetMin, src.TargetMin)
	assignFloat(&dst.TargetMax, src.TargetMax)
	assignFloat(&dst.StepUp, src.StepUp)
	assignFloat(&dst.StepDown, src.StepDown)
	assignFloat(&dst.FallbackTarget, src.FallbackTarget)
	assignFloat(&dst.GoalLow, src.GoalLow)
	assignFloat(&dst.GoalHigh, src.GoalHigh)
	assignDuration(&dst.Interval, src.Interval)
	assignDuration(&dst.RelaxedInterval, src.RelaxedInterval)
	assignFloat(&dst.RelaxedThreshold, src.RelaxedThreshold)
}

func mergeEstimatorConfig(dst *estimatorConfig, src estimatorFileConfig) {
	assignDuration(&dst.Interval, src.Interval)
}

func mergePoolConfig(dst *poolConfig, src poolFileConfig) {
	assignInt(&dst.Workers, src.Workers)
	assignDuration(&dst.Quantum, src.Quantum)
}

func mergeHTTPConfig(dst *httpConfig, src httpFileConfig) {
	assignString(&dst.Bind, src.Bind)
}

func mergeOCIConfig(dst *ociConfig, src ociFileConfig) {
	assignString(&dst.CompartmentID, src.CompartmentID)
}

func applyEnvOverrides(cfg *runtimeConfig) {
	cfg.Controller.TargetStart = envFloat(envTargetStart, cfg.Controller.TargetStart)
	cfg.Controller.TargetMin = envFloat(envTargetMin, cfg.Controller.TargetMin)
	cfg.Controller.TargetMax = envFloat(envTargetMax, cfg.Controller.TargetMax)
	cfg.Controller.StepUp = envFloat(envStepUp, cfg.Controller.StepUp)
	cfg.Controller.StepDown = envFloat(envStepDown, cfg.Controller.StepDown)
	cfg.Controller.FallbackTarget = envFloat(envFallbackTarget, cfg.Controller.FallbackTarget)
	cfg.Controller.GoalLow = envFloat(envGoalLow, cfg.Controller.GoalLow)
	cfg.Controller.GoalHigh = envFloat(envGoalHigh, cfg.Controller.GoalHigh)
	cfg.Controller.RelaxedThreshold = envFloat(envRelaxedThreshold, cfg.Controller.RelaxedThreshold)
	cfg.Controller.Interval = envDuration(envSlowInterval, cfg.Controller.Interval)
	cfg.Controller.RelaxedInterval = envDuration(envRelaxedInterval, cfg.Controller.RelaxedInterval)
	cfg.Estimator.Interval = envDuration(envFastInterval, cfg.Estimator.Interval)
	cfg.Pool.Workers = envInt(envPoolWorkers, cfg.Pool.Workers)
	cfg.HTTP.Bind = envString(envHTTPBind, cfg.HTTP.Bind)
	cfg.OCI.CompartmentID = envString(envCompartmentID, cfg.OCI.CompartmentID)

	defaults := adapt.DefaultConfig()

	if cfg.Pool.Workers <= 0 {
		cfg.Pool.Workers = 1
	}

	if cfg.Pool.Quantum <= 0 {
		cfg.Pool.Quantum = shape.DefaultQuantum
	}

	if cfg.Controller.Interval <= 0 {
		cfg.Controller.Interval = defaults.Interval
	}

	if cfg.Controller.RelaxedInterval <= 0 {
		cfg.Controller.RelaxedInterval = defaults.RelaxedInterval
	}

	if cfg.Estimator.Interval <= 0 {
		cfg.Estimator.Interval = time.Second
	}
}

var lookupEnv = os.LookupEnv //nolint:gochecknoglobals // overridden in tests

func parseFloatDefault(value string, fallback float64) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return fallback
	}

	return parsed
}

func assignFloat(target *float64, value *float64) {
	if value != nil {
		*target = *value
	}
}

func assignDuration(target *time.Duration, value *time.Duration) {
	if value != nil {
		*target = *value
	}
}

func assignInt(target *int, value *int) {
	if value != nil {
		*target = *value
	}
}

func assignString(target *string, value *string) {
	if value != nil {
		*target = strings.TrimSpace(*value)
	}
}

func envFloat(key string, fallback float64) float64 {
	value, ok := lookupEnv(key)
	if !ok {
		return fallback
	}

	return parseFloatDefault(value, fallback)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, ok := lookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return fallback
	}

	return duration
}

func envInt(key string, fallback int) int {
	value, ok := lookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func envString(key, fallback string) string {
	value, ok := lookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}
