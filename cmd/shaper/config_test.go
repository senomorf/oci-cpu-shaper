package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/adapt"
)

const testCompartmentOverride = "ocid1.compartment.oc1..override"

func TestLoadConfigDefaultsWhenFileMissing(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfig("./testdata/missing.yaml")
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if cfg.Controller.TargetStart != adaptDefault().TargetStart {
		t.Fatalf("unexpected targetStart: %v", cfg.Controller.TargetStart)
	}

	if cfg.HTTP.Bind != ":9108" {
		t.Fatalf("unexpected http bind address: %q", cfg.HTTP.Bind)
	}

	if cfg.Estimator.Interval != time.Second {
		t.Fatalf("unexpected estimator interval: %v", cfg.Estimator.Interval)
	}
}

func TestLoadConfigAppliesFileOverrides(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "config.yaml")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if cfg.Controller.TargetStart != 0.26 {
		t.Fatalf("expected targetStart override, got %v", cfg.Controller.TargetStart)
	}

	if cfg.Controller.Interval != 30*time.Minute {
		t.Fatalf("expected controller interval override, got %v", cfg.Controller.Interval)
	}

	if cfg.Pool.Workers != 2 {
		t.Fatalf("expected pool workers override, got %d", cfg.Pool.Workers)
	}

	if cfg.HTTP.Bind != ":9200" {
		t.Fatalf("expected http bind override, got %q", cfg.HTTP.Bind)
	}

	expectedCompartment := "ocid1.compartment.oc1..exampleuniqueID"
	if cfg.OCI.CompartmentID != expectedCompartment {
		t.Fatalf("expected compartment id %q, got %q", expectedCompartment, cfg.OCI.CompartmentID)
	}

	expectedInstance := "ocid1.instance.oc1..config"
	if cfg.OCI.InstanceID != expectedInstance {
		t.Fatalf("expected instance id %q, got %q", expectedInstance, cfg.OCI.InstanceID)
	}
}

func TestLoadConfigAppliesEnvOverrides(t *testing.T) {
	t.Setenv(envTargetStart, "0.33")
	t.Setenv(envTargetMin, "0.20")
	t.Setenv(envStepUp, "0.05")
	t.Setenv(envSlowInterval, "2h")
	t.Setenv(envRelaxedInterval, "12h")
	t.Setenv(envFastInterval, "250ms")
	t.Setenv(envPoolWorkers, "4")
	t.Setenv(envHTTPBind, " :9300 ")
	t.Setenv(envCompartmentID, " "+testCompartmentOverride+" ")
	t.Setenv(envInstanceID, " ocid1.instance.oc1..override ")

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if cfg.Controller.TargetStart != 0.33 {
		t.Fatalf("expected env override for targetStart, got %v", cfg.Controller.TargetStart)
	}

	if cfg.Controller.Interval != 2*time.Hour {
		t.Fatalf("expected env override for interval, got %v", cfg.Controller.Interval)
	}

	if cfg.Controller.RelaxedInterval != 12*time.Hour {
		t.Fatalf(
			"expected env override for relaxed interval, got %v",
			cfg.Controller.RelaxedInterval,
		)
	}

	if cfg.Estimator.Interval != 250*time.Millisecond {
		t.Fatalf("expected env override for estimator interval, got %v", cfg.Estimator.Interval)
	}

	if cfg.Pool.Workers != 4 {
		t.Fatalf("expected env override for workers, got %d", cfg.Pool.Workers)
	}

	if cfg.HTTP.Bind != ":9300" {
		t.Fatalf("expected env override for http bind, got %q", cfg.HTTP.Bind)
	}

	if cfg.OCI.CompartmentID != testCompartmentOverride {
		t.Fatalf("expected env override for compartment ID, got %q", cfg.OCI.CompartmentID)
	}

	if cfg.OCI.InstanceID != "ocid1.instance.oc1..override" {
		t.Fatalf("expected env override for instance ID, got %q", cfg.OCI.InstanceID)
	}
}

func TestLoadConfigReturnsDecodeError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	writeErr := os.WriteFile(path, []byte("controller: ["), 0o600)
	if writeErr != nil {
		t.Fatalf("write temp file: %v", writeErr)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

func adaptDefault() adapt.Config {
	return adapt.DefaultConfig()
}
