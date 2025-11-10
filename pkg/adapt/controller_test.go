package adapt

import (
	"context"
	"testing"
)

func TestNewNoopControllerDefaultsMode(t *testing.T) {
	t.Parallel()

	controller := NewNoopController("")
	if controller.Mode() != "noop" {
		t.Fatalf("expected noop mode, got %q", controller.Mode())
	}
}

func TestNoopControllerRunIsNonBlocking(t *testing.T) {
	t.Parallel()

	controller := NewNoopController("dry-run")
	if controller.Mode() != "dry-run" {
		t.Fatalf("expected mode to be preserved, got %q", controller.Mode())
	}

	if err := controller.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}
