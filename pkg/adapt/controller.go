// Package adapt hosts the shaping controllers responsible for policy execution.
package adapt

import "context"

// Controller coordinates CPU shaping strategies.
type Controller interface {
	// Run applies the configured shaping logic until completion or context cancellation.
	Run(ctx context.Context) error
	// Mode exposes the configured controller mode (e.g., dry-run or enforce).
	Mode() string
}

// NoopController is a bootstrap controller that records the selected mode and exits immediately.
type NoopController struct {
	mode string
}

// NewNoopController constructs a controller placeholder used during early development.
func NewNoopController(mode string) *NoopController {
	if mode == "" {
		mode = "noop"
	}

	return &NoopController{mode: mode}
}

// Run satisfies the Controller interface without performing any work.
func (c *NoopController) Run(ctx context.Context) error {
	_ = ctx
	return nil
}

// Mode reports the configured operating mode for this controller.
func (c *NoopController) Mode() string {
	return c.mode
}
