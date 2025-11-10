package adapt_test

import (
	"testing"

	"oci-cpu-shaper/pkg/adapt"
)

func TestNewNoopControllerDefaultsMode(t *testing.T) {
	t.Parallel()

	controller := adapt.NewNoopController("")
	if controller.Mode() != "noop" {
		t.Fatalf("expected noop mode, got %q", controller.Mode())
	}
}

func TestNoopControllerRunIsNonBlocking(t *testing.T) {
	t.Parallel()

	controller := adapt.NewNoopController("dry-run")
	if controller.Mode() != "dry-run" {
		t.Fatalf("expected mode to be preserved, got %q", controller.Mode())
	}

	if err := controller.Run(t.Context()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}
