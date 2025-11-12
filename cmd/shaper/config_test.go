package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/adapt"
)

const (
	testCompartmentOverride = "ocid1.compartment.oc1..override"
	testRegionOverride      = "us-sanjose-1"
)

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

	if cfg.OCI.Offline {
		t.Fatal("expected offline mode to default to false")
	}

	if cfg.OCI.Region != "" {
		t.Fatalf("expected region to default empty, got %q", cfg.OCI.Region)
	}

	assertFloatEqual(
		t,
		"suppressThreshold",
		cfg.Controller.SuppressThreshold,
		adaptDefault().SuppressThreshold,
	)
	assertFloatEqual(
		t,
		"suppressResume",
		cfg.Controller.SuppressResume,
		adaptDefault().SuppressResume,
	)
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

	expectedRegion := stubRegion
	if cfg.OCI.Region != expectedRegion {
		t.Fatalf("expected region %q, got %q", expectedRegion, cfg.OCI.Region)
	}

	assertFloatEqual(t, "suppressThreshold", cfg.Controller.SuppressThreshold, 0.9)
	assertFloatEqual(t, "suppressResume", cfg.Controller.SuppressResume, 0.6)
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
	t.Setenv(envOCIRegion, " "+testRegionOverride+" ")
	t.Setenv(envOCIOffline, "true")
	t.Setenv(envSuppressThreshold, "0.88")
	t.Setenv(envSuppressResume, "0.51")

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	assertFloatEqual(t, "targetStart", cfg.Controller.TargetStart, 0.33)
	assertFloatEqual(t, "targetMin", cfg.Controller.TargetMin, 0.20)
	assertFloatEqual(t, "stepUp", cfg.Controller.StepUp, 0.05)
	assertDurationEqual(t, "interval", cfg.Controller.Interval, 2*time.Hour)
	assertDurationEqual(t, "relaxedInterval", cfg.Controller.RelaxedInterval, 12*time.Hour)
	assertFloatEqual(t, "suppressThreshold", cfg.Controller.SuppressThreshold, 0.88)
	assertFloatEqual(t, "suppressResume", cfg.Controller.SuppressResume, 0.51)
	assertDurationEqual(t, "estimatorInterval", cfg.Estimator.Interval, 250*time.Millisecond)
	assertIntEqual(t, "workers", cfg.Pool.Workers, 4)
	assertStringEqual(t, "httpBind", cfg.HTTP.Bind, ":9300")
	assertStringEqual(t, "compartmentID", cfg.OCI.CompartmentID, testCompartmentOverride)
	assertStringEqual(t, "region", cfg.OCI.Region, testRegionOverride)
	assertStringEqual(t, "instanceID", cfg.OCI.InstanceID, "ocid1.instance.oc1..override")
	assertBoolEqual(t, "offline", cfg.OCI.Offline, true)
}

func TestLoadConfigRejectsTargetsExceedingSuppressThreshold(t *testing.T) {
	t.Setenv(envSuppressThreshold, "0.35")
	t.Setenv(envSuppressResume, "0.34")

	_, err := loadConfig("")
	if err == nil {
		t.Fatal("expected validation error when suppressThreshold is below target values")
	}

	if !errors.Is(err, adapt.ErrInvalidConfig) {
		t.Fatalf("expected adapt.ErrInvalidConfig, got %v", err)
	}

	if !strings.Contains(err.Error(), "controller.targetMax") {
		t.Fatalf("expected error to reference controller.targetMax, got %v", err)
	}
}

func TestLoadConfigRejectsTargetsExceedingSuppressResume(t *testing.T) {
	t.Setenv(envSuppressResume, "0.10")

	_, err := loadConfig("")
	if err == nil {
		t.Fatal("expected validation error when suppressResume is below target values")
	}

	if !errors.Is(err, adapt.ErrInvalidConfig) {
		t.Fatalf("expected adapt.ErrInvalidConfig, got %v", err)
	}

	if !strings.Contains(err.Error(), "controller.targetStart") {
		t.Fatalf("expected error to reference controller.targetStart, got %v", err)
	}
}

func TestLoadRuntimeConfigOrExitReturnsParseCodeOnValidationError(t *testing.T) {
	t.Parallel()

	var deps runDeps

	deps.loadConfig = func(string) (runtimeConfig, error) {
		return runtimeConfig{}, fmt.Errorf("wrap: %w", adapt.ErrInvalidConfig)
	}

	var stderr bytes.Buffer

	_, exitCode, loaded := loadRuntimeConfigOrExit(deps, "", &stderr)
	if loaded {
		t.Fatal("expected loadRuntimeConfigOrExit to report failure")
	}

	if exitCode != exitCodeParseError {
		t.Fatalf("expected parse error exit code %d, got %d", exitCodeParseError, exitCode)
	}

	output := stderr.String()
	if !strings.Contains(output, "failed to load configuration") {
		t.Fatalf("expected diagnostic output, got %q", output)
	}
}

//nolint:paralleltest // manipulates shared lookupEnv globally
func TestLoadConfigAppliesOfflineFileOverride(t *testing.T) {
	path := filepath.Join("testdata", "offline.yaml")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if !cfg.OCI.Offline {
		t.Fatal("expected offline flag to be enabled from file config")
	}

	expectedInstance := "ocid1.instance.oc1..offline"
	if cfg.OCI.InstanceID != expectedInstance {
		t.Fatalf("unexpected instance id override: %q", cfg.OCI.InstanceID)
	}

	expectedOverride := "ocid1.instance.oc1..override"
	origLookupEnv := lookupEnv

	t.Cleanup(func() {
		lookupEnv = origLookupEnv
	})

	lookupEnv = func(key string) (string, bool) {
		if key == envInstanceID {
			return " " + expectedOverride + " ", true
		}

		return origLookupEnv(key)
	}

	overrideCfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if !overrideCfg.OCI.Offline {
		t.Fatal("expected offline flag to remain enabled after env override")
	}

	if overrideCfg.OCI.InstanceID != expectedOverride {
		t.Fatalf("expected env override for instance ID, got %q", overrideCfg.OCI.InstanceID)
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

func assertFloatEqual(t *testing.T, name string, got, want float64) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s override %v, got %v", name, want, got)
	}
}

func assertDurationEqual(t *testing.T, name string, got, want time.Duration) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s override %v, got %v", name, want, got)
	}
}

func assertIntEqual(t *testing.T, name string, got, want int) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s override %d, got %d", name, want, got)
	}
}

func assertStringEqual(t *testing.T, name, got, want string) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s override %q, got %q", name, want, got)
	}
}

func assertBoolEqual(t *testing.T, name string, got, want bool) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s override %t, got %t", name, want, got)
	}
}
