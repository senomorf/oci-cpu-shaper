package adapt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"oci-cpu-shaper/pkg/est"
	"oci-cpu-shaper/pkg/oci"
)

// State captures the controller operating mode.
type State int

const (
	// StateNormal represents steady-state operation using OCI feedback.
	StateNormal State = iota
	// StateFallback is entered when OCI metrics are unavailable.
	StateFallback
)

// Controller represents the adaptive control loop surface.
type Controller interface {
	Run(ctx context.Context) error
	Mode() string
}

// DutyCycler is implemented by the shape worker pool.
type DutyCycler interface {
	SetTarget(target float64)
	Target() float64
}

// Estimator exposes the observation stream produced by pkg/est.
type Estimator interface {
	Run(ctx context.Context) <-chan est.Observation
}

// Config defines controller thresholds.
type Config struct {
	ResourceID       string
	Mode             string
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

// DefaultConfig mirrors the initial implementation plan for control loop cadence.
const (
	defaultModeLabel       = "normal"
	defaultTargetStart     = 0.25
	defaultTargetMin       = 0.22
	defaultTargetMax       = 0.40
	defaultStepUp          = 0.02
	defaultStepDown        = 0.01
	defaultFallbackTarget  = 0.25
	defaultGoalLow         = 0.23
	defaultGoalHigh        = 0.30
	defaultRelaxedInterval = 6 * time.Hour
	defaultRelaxedThresh   = 0.28
)

func DefaultConfig() Config {
	return Config{
		ResourceID:       "",
		Mode:             defaultModeLabel,
		TargetStart:      defaultTargetStart,
		TargetMin:        defaultTargetMin,
		TargetMax:        defaultTargetMax,
		StepUp:           defaultStepUp,
		StepDown:         defaultStepDown,
		FallbackTarget:   defaultFallbackTarget,
		GoalLow:          defaultGoalLow,
		GoalHigh:         defaultGoalHigh,
		Interval:         time.Hour,
		RelaxedInterval:  defaultRelaxedInterval,
		RelaxedThreshold: defaultRelaxedThresh,
	}
}

var (
	errMetricsClientRequired = errors.New("adapt: metrics client is required")
	errDutyCyclerRequired    = errors.New("adapt: duty cycler is required")
)

// AdaptiveController orchestrates the normal/fallback state machine.
type AdaptiveController struct {
	cfg       Config
	metrics   oci.MetricsClient
	shaper    DutyCycler
	estimator Estimator

	mu       sync.Mutex
	state    State
	target   float64
	lastP95  float64
	lastErr  error
	interval time.Duration
	mode     string
}

var _ Controller = (*AdaptiveController)(nil)

// NewAdaptiveController wires together the OCI metrics client, estimator and shaper.
func NewAdaptiveController(
	cfg Config,
	metrics oci.MetricsClient,
	estimator Estimator,
	shaper DutyCycler,
) (*AdaptiveController, error) {
	if metrics == nil {
		return nil, errMetricsClientRequired
	}

	if shaper == nil {
		return nil, errDutyCyclerRequired
	}

	normalized, mode := normalizeConfig(cfg)

	controller := new(AdaptiveController)
	controller.cfg = normalized
	controller.metrics = metrics
	controller.shaper = shaper
	controller.estimator = estimator
	controller.state = StateFallback
	controller.target = normalized.FallbackTarget
	controller.interval = normalized.Interval
	controller.mode = mode

	shaper.SetTarget(normalized.FallbackTarget)

	return controller, nil
}

// Run executes the control loop until the context is cancelled.
func (c *AdaptiveController) Run(ctx context.Context) error {
	if c.estimator != nil {
		go c.consumeEstimator(ctx, c.estimator.Run(ctx))
	}

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if err != nil {
				return fmt.Errorf("adaptive controller run: %w", err)
			}

			return nil
		case <-ticker.C:
			nextInterval := c.step(ctx)
			if nextInterval <= 0 {
				nextInterval = c.cfg.Interval
			}

			if nextInterval != c.interval {
				ticker.Reset(nextInterval)
			}

			c.mu.Lock()
			c.interval = nextInterval
			c.mu.Unlock()
		}
	}
}

// State returns the current controller state.
func (c *AdaptiveController) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state
}

// Target returns the shaper target tracked by the controller.
func (c *AdaptiveController) Target() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.target
}

// LastP95 returns the last successful OCI P95 value.
func (c *AdaptiveController) LastP95() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastP95
}

// Mode returns the configured controller mode label.
func (c *AdaptiveController) Mode() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.mode
}

func (c *AdaptiveController) consumeEstimator(ctx context.Context, ch <-chan est.Observation) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			// Host CPU observations are currently used for telemetry only.
		}
	}
}

func (c *AdaptiveController) step(ctx context.Context) time.Duration {
	p95, err := c.metrics.QueryP95CPU(ctx, c.cfg.ResourceID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.state = StateFallback
		c.lastErr = err
		c.target = clamp(c.cfg.FallbackTarget, c.cfg.TargetMin, c.cfg.TargetMax)
		c.shaper.SetTarget(c.target)

		return c.cfg.Interval
	}

	c.state = StateNormal
	c.lastErr = nil
	c.lastP95 = p95

	nextTarget := c.target
	if nextTarget == 0 {
		nextTarget = c.cfg.TargetStart
	}

	if p95 < c.cfg.GoalLow {
		nextTarget += c.cfg.StepUp
	} else if p95 > c.cfg.GoalHigh {
		nextTarget -= c.cfg.StepDown
	}

	nextTarget = clamp(nextTarget, c.cfg.TargetMin, c.cfg.TargetMax)
	c.target = nextTarget
	c.shaper.SetTarget(nextTarget)

	if p95 >= c.cfg.RelaxedThreshold {
		return c.cfg.RelaxedInterval
	}

	return c.cfg.Interval
}

func clamp(value, lower, upper float64) float64 {
	if value < lower {
		return lower
	}

	if value > upper {
		return upper
	}

	return value
}

// NoopController satisfies the Controller interface but performs no work.
type NoopController struct {
	mode string
}

var _ Controller = (*NoopController)(nil)

// NewNoopController builds a controller that immediately returns without work.
func NewNoopController(mode string) *NoopController {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		trimmed = "noop"
	}

	return &NoopController{mode: trimmed}
}

// Run implements the Controller interface.
func (n *NoopController) Run(context.Context) error { return nil }

// Mode implements the Controller interface.
func (n *NoopController) Mode() string { return n.mode }

func normalizeConfig(cfg Config) (Config, string) {
	defaults := DefaultConfig()

	cfg.Interval = ensureDuration(cfg.Interval, defaults.Interval)
	cfg.RelaxedInterval = ensureDuration(cfg.RelaxedInterval, defaults.RelaxedInterval)
	cfg.TargetStart = ensureFloat(cfg.TargetStart, defaults.TargetStart)
	cfg.TargetMin = ensureFloat(cfg.TargetMin, defaults.TargetMin)
	cfg.TargetMax = ensureFloat(cfg.TargetMax, defaults.TargetMax)
	cfg.StepUp = ensureFloat(cfg.StepUp, defaults.StepUp)
	cfg.StepDown = ensureFloat(cfg.StepDown, defaults.StepDown)
	cfg.FallbackTarget = ensureFloat(cfg.FallbackTarget, defaults.FallbackTarget)
	cfg.GoalLow = ensureFloat(cfg.GoalLow, defaults.GoalLow)
	cfg.GoalHigh = ensureFloat(cfg.GoalHigh, defaults.GoalHigh)
	cfg.RelaxedThreshold = ensureFloat(cfg.RelaxedThreshold, defaults.RelaxedThreshold)

	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = defaultModeLabel
	}

	return cfg, mode
}

func ensureDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}

	return value
}

func ensureFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}

	return value
}
