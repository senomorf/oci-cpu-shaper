package adapt

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	// StateSuppressed is entered when the fast estimator detects host contention.
	StateSuppressed
)

// String implements fmt.Stringer for State values.
func (s State) String() string {
	switch s {
	case StateNormal:
		return "normal"
	case StateFallback:
		return "fallback"
	case StateSuppressed:
		return "suppressed"
	default:
		return "unknown"
	}
}

// Controller represents the adaptive control loop surface.
type Controller interface {
	Run(ctx context.Context) error
	Mode() string
	State() State
	LastError() error
	LastEstimatorError() error
}

// DutyCycler is implemented by the shape worker pool.
type DutyCycler interface {
	SetTarget(target float64)
	Target() float64
}

// MetricsRecorder captures controller observability signals.
type MetricsRecorder interface {
	SetMode(mode string)
	SetState(state string)
	SetTarget(target float64)
	ObserveOCIP95(value float64, fetchedAt time.Time)
	ObserveHostCPU(utilisation float64)
}

// Estimator exposes the observation stream produced by pkg/est.
type Estimator interface {
	Run(ctx context.Context) <-chan est.Observation
}

// Config defines controller thresholds.
type Config struct {
	ResourceID        string
	Mode              string
	TargetStart       float64
	TargetMin         float64
	TargetMax         float64
	StepUp            float64
	StepDown          float64
	FallbackTarget    float64
	GoalLow           float64
	GoalHigh          float64
	Interval          time.Duration
	RelaxedInterval   time.Duration
	RelaxedThreshold  float64
	SuppressThreshold float64
	SuppressResume    float64
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
	defaultSuppressThresh  = 0.85
	defaultSuppressResume  = 0.70
	hostLoadSmoothing      = 5
	suppressResumeScale    = 0.8
)

func DefaultConfig() Config {
	return Config{
		ResourceID:        "",
		Mode:              defaultModeLabel,
		TargetStart:       defaultTargetStart,
		TargetMin:         defaultTargetMin,
		TargetMax:         defaultTargetMax,
		StepUp:            defaultStepUp,
		StepDown:          defaultStepDown,
		FallbackTarget:    defaultFallbackTarget,
		GoalLow:           defaultGoalLow,
		GoalHigh:          defaultGoalHigh,
		Interval:          time.Hour,
		RelaxedInterval:   defaultRelaxedInterval,
		RelaxedThreshold:  defaultRelaxedThresh,
		SuppressThreshold: defaultSuppressThresh,
		SuppressResume:    defaultSuppressResume,
	}
}

var (
	errMetricsClientRequired = errors.New("adapt: metrics client is required")
	errDutyCyclerRequired    = errors.New("adapt: duty cycler is required")
	// ErrInvalidConfig signals that the supplied controller configuration is invalid.
	ErrInvalidConfig = errors.New("adapt: invalid config")
)

// AdaptiveController orchestrates the normal/fallback state machine.
type AdaptiveController struct {
	cfg       Config
	metrics   oci.MetricsClient
	shaper    DutyCycler
	estimator Estimator
	recorder  MetricsRecorder

	mu         sync.Mutex
	state      State
	slowState  State
	suppressed bool
	target     float64
	desired    float64
	lastP95    float64
	lastErr    error
	lastEstErr error
	hostLoad   float64
	interval   time.Duration
	mode       string
}

var _ Controller = (*AdaptiveController)(nil)

// NewAdaptiveController wires together the OCI metrics client, estimator and shaper.
func NewAdaptiveController(
	cfg Config,
	metrics oci.MetricsClient,
	estimator Estimator,
	shaper DutyCycler,
	recorder MetricsRecorder,
) (*AdaptiveController, error) {
	if metrics == nil {
		return nil, errMetricsClientRequired
	}

	if shaper == nil {
		return nil, errDutyCyclerRequired
	}

	normalized, mode, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	controller := new(AdaptiveController)
	controller.cfg = normalized
	controller.metrics = metrics
	controller.shaper = shaper
	controller.estimator = estimator
	controller.recorder = recorder
	controller.state = StateFallback
	controller.slowState = StateFallback
	controller.target = normalized.FallbackTarget
	controller.desired = normalized.FallbackTarget
	controller.interval = normalized.Interval
	controller.mode = mode

	shaper.SetTarget(normalized.FallbackTarget)

	if recorder != nil {
		recorder.SetMode(mode)
		recorder.SetState(controller.state.String())
		recorder.SetTarget(controller.target)
	}

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

// LastError returns the most recent OCI metrics error encountered by the controller.
func (c *AdaptiveController) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastErr
}

// LastP95 returns the last successful OCI P95 value.
func (c *AdaptiveController) LastP95() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastP95
}

// LastEstimatorError returns the last observation error from the fast estimator loop.
func (c *AdaptiveController) LastEstimatorError() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastEstErr
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
		case observation, ok := <-ch:
			if !ok {
				return
			}

			c.handleObservation(observation)
		}
	}
}

func (c *AdaptiveController) handleObservation(observation est.Observation) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if observation.Err != nil {
		c.lastEstErr = observation.Err
		c.updateEffectiveStateLocked()

		return
	}

	c.lastEstErr = nil

	if c.cfg.SuppressThreshold <= 0 {
		return
	}

	utilisation := clamp(observation.Utilisation, 0, 1)
	if c.recorder != nil {
		c.recorder.ObserveHostCPU(utilisation)
	}

	c.updateHostLoadLocked(utilisation)
	previouslySuppressed := c.transitionSuppressionLocked()
	c.applySuppressionTargetsLocked(previouslySuppressed)
	c.updateEffectiveStateLocked()
}

func (c *AdaptiveController) updateHostLoadLocked(utilisation float64) {
	if c.hostLoad == 0 {
		c.hostLoad = utilisation

		return
	}

	c.hostLoad += (utilisation - c.hostLoad) / float64(hostLoadSmoothing)
}

func (c *AdaptiveController) transitionSuppressionLocked() bool {
	previous := c.suppressed

	if !c.suppressed && c.hostLoad >= c.cfg.SuppressThreshold {
		c.suppressed = true
	} else if c.suppressed && c.hostLoad <= c.cfg.SuppressResume {
		c.suppressed = false
	}

	return previous
}

func (c *AdaptiveController) applySuppressionTargetsLocked(previouslySuppressed bool) {
	switch {
	case c.suppressed:
		c.applyTargetLocked(0)
	case previouslySuppressed:
		restore := c.desired
		if restore == 0 {
			restore = c.cfg.TargetStart
		}

		restore = clamp(restore, c.cfg.TargetMin, c.cfg.TargetMax)
		c.applyTargetLocked(restore)
	}
}

func (c *AdaptiveController) step(ctx context.Context) time.Duration {
	p95, err := c.metrics.QueryP95CPU(ctx, c.cfg.ResourceID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.slowState = StateFallback
		c.lastErr = err
		fallback := clamp(c.cfg.FallbackTarget, c.cfg.TargetMin, c.cfg.TargetMax)

		c.desired = fallback
		if !c.suppressed {
			c.applyTargetLocked(fallback)
		}

		c.updateEffectiveStateLocked()

		return c.cfg.Interval
	}

	c.slowState = StateNormal
	c.lastErr = nil

	c.lastP95 = p95
	if c.recorder != nil {
		c.recorder.ObserveOCIP95(p95, time.Now())
	}

	nextTarget := c.target
	if c.suppressed {
		nextTarget = c.desired
	}

	if nextTarget == 0 {
		nextTarget = c.cfg.TargetStart
	}

	if p95 < c.cfg.GoalLow {
		nextTarget += c.cfg.StepUp
	} else if p95 > c.cfg.GoalHigh {
		nextTarget -= c.cfg.StepDown
	}

	nextTarget = clamp(nextTarget, c.cfg.TargetMin, c.cfg.TargetMax)

	c.desired = nextTarget
	if !c.suppressed {
		c.applyTargetLocked(nextTarget)
	}

	c.updateEffectiveStateLocked()

	if p95 >= c.cfg.RelaxedThreshold {
		return c.cfg.RelaxedInterval
	}

	return c.cfg.Interval
}

func (c *AdaptiveController) applyTargetLocked(target float64) {
	c.target = target
	c.shaper.SetTarget(target)

	if c.recorder != nil {
		c.recorder.SetTarget(target)
	}
}

func (c *AdaptiveController) updateEffectiveStateLocked() {
	if c.suppressed {
		c.state = StateSuppressed
		if c.recorder != nil {
			c.recorder.SetState(c.state.String())
		}

		return
	}

	c.state = c.slowState
	if c.recorder != nil {
		c.recorder.SetState(c.state.String())
	}
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

// State implements the Controller interface.
func (n *NoopController) State() State { return StateNormal }

// LastError implements the Controller interface.
func (n *NoopController) LastError() error { return nil }

// LastEstimatorError implements the Controller interface.
func (n *NoopController) LastEstimatorError() error { return nil }

func normalizeConfig(cfg Config) (Config, string, error) {
	normalized, mode := coerceConfig(cfg)

	err := validateControllerConfig(normalized)
	if err != nil {
		return Config{}, "", err
	}

	return normalized, mode, nil
}

// ValidateConfig ensures controller thresholds are internally consistent.
func ValidateConfig(cfg Config) error {
	normalized, _ := coerceConfig(cfg)

	return validateControllerConfig(normalized)
}

func coerceConfig(cfg Config) (Config, string) {
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
	cfg.SuppressThreshold = ensureFloat(cfg.SuppressThreshold, defaults.SuppressThreshold)
	cfg.SuppressResume = ensureFloat(cfg.SuppressResume, defaults.SuppressResume)

	cfg.SuppressThreshold = clamp(cfg.SuppressThreshold, 0, 1)
	cfg.SuppressResume = clamp(cfg.SuppressResume, 0, 1)

	if cfg.SuppressResume >= cfg.SuppressThreshold && cfg.SuppressThreshold > 0 {
		cfg.SuppressResume = math.Max(cfg.SuppressThreshold*suppressResumeScale, 0)
	}

	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = defaultModeLabel
	}

	return cfg, mode
}

func validateControllerConfig(cfg Config) error {
	thresholds := []struct {
		name  string
		value float64
	}{
		{"controller.targetStart", cfg.TargetStart},
		{"controller.targetMin", cfg.TargetMin},
		{"controller.targetMax", cfg.TargetMax},
		{"controller.fallbackTarget", cfg.FallbackTarget},
		{"controller.goalLow", cfg.GoalLow},
		{"controller.goalHigh", cfg.GoalHigh},
	}

	for _, threshold := range thresholds {
		if cfg.SuppressThreshold <= threshold.value {
			return fmt.Errorf(
				"%w: controller.suppressThreshold (%.2f) must be greater than %s (%.2f)",
				ErrInvalidConfig,
				cfg.SuppressThreshold,
				threshold.name,
				threshold.value,
			)
		}

		if cfg.SuppressResume <= threshold.value {
			return fmt.Errorf(
				"%w: controller.suppressResume (%.2f) must be greater than %s (%.2f)",
				ErrInvalidConfig,
				cfg.SuppressResume,
				threshold.name,
				threshold.value,
			)
		}
	}

	return nil
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
