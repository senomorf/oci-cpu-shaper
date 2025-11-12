package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"oci-cpu-shaper/pkg/oci"
)

var (
	errQueryFailure   = errors.New("boom")
	errFactoryFailure = errors.New("factory failure")

	metricsClientMutex sync.Mutex //nolint:gochecknoglobals // test seam
)

type fakeMetricsClient struct {
	mu        sync.Mutex
	values    []float32
	lastArgs  []any
	err       error
	callCount int
}

func (f *fakeMetricsClient) QueryP95CPU(
	_ context.Context,
	instanceOCID string,
	last7d bool,
) (float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callCount++
	f.lastArgs = []any{instanceOCID, last7d}

	if len(f.values) > 0 {
		return f.values[0], f.err
	}

	return 0, f.err
}

func withMetricsClient(t *testing.T, client metricsQuerier, execute func()) {
	t.Helper()

	metricsClientMutex.Lock()

	previousFactory := newMetricsClient
	newMetricsClient = func(string, string) (metricsQuerier, error) {
		return client, nil
	}

	defer func() {
		newMetricsClient = previousFactory

		metricsClientMutex.Unlock()
	}()

	execute()
}

func captureLogs(t *testing.T, execute func()) string {
	t.Helper()

	var buffer bytes.Buffer

	previousWriter := log.Writer()
	previousFlags := log.Flags()

	log.SetOutput(&buffer)
	log.SetFlags(0)

	defer func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}()

	execute()

	return buffer.String()
}

func TestParseConfigUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if !cfg.last7d {
		t.Fatalf("expected last7d default true, got %v", cfg.last7d)
	}

	if cfg.timeout != defaultTimeout {
		t.Fatalf("expected default timeout, got %v", cfg.timeout)
	}

	if cfg.allowEmpty {
		t.Fatalf("expected allowEmpty default false")
	}
}

func TestParseConfigParsesFlags(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]string{
		"-instance", "ocid1.instance.oc1..exampleuniqueID",
		"-compartment", "ocid1.compartment.oc1..exampleuniqueID",
		"-region", "us-phoenix-1",
		"-timeout", "45s",
		"-allow-empty",
		"-last7d=false",
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if cfg.instanceID != "ocid1.instance.oc1..exampleuniqueID" {
		t.Fatalf("unexpected instance ID: %s", cfg.instanceID)
	}

	if cfg.compartmentID != "ocid1.compartment.oc1..exampleuniqueID" {
		t.Fatalf("unexpected compartment ID: %s", cfg.compartmentID)
	}

	if cfg.region != "us-phoenix-1" {
		t.Fatalf("unexpected region: %s", cfg.region)
	}

	if cfg.timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %v", cfg.timeout)
	}

	if !cfg.allowEmpty {
		t.Fatalf("expected allowEmpty to be true")
	}

	if cfg.last7d {
		t.Fatalf("expected last7d to be false")
	}
}

func TestRunQueryRequiresInstanceID(t *testing.T) {
	t.Parallel()

	err := runQuery(queryConfig{
		instanceID:    "",
		compartmentID: "",
		region:        "",
		last7d:        true,
		timeout:       defaultTimeout,
		allowEmpty:    false,
	})
	if !errors.Is(err, errMissingInstance) {
		t.Fatalf("expected errMissingInstance, got %v", err)
	}
}

func TestRunQueryRequiresCompartmentID(t *testing.T) {
	t.Parallel()

	err := runQuery(queryConfig{
		instanceID:    "ocid1.instance",
		compartmentID: "",
		region:        "",
		last7d:        true,
		timeout:       defaultTimeout,
		allowEmpty:    false,
	})
	if !errors.Is(err, errMissingCompartment) {
		t.Fatalf("expected errMissingCompartment, got %v", err)
	}
}

func TestRunQueryLogsValue(t *testing.T) {
	t.Parallel()

	client := &fakeMetricsClient{ //nolint:exhaustruct
		values: []float32{12.5},
	}

	withMetricsClient(t, client, func() {
		output := captureLogs(t, func() {
			err := runQuery(queryConfig{
				instanceID:    "ocid1.instance",
				compartmentID: "ocid1.compartment",
				region:        "",
				last7d:        true,
				timeout:       time.Second,
				allowEmpty:    false,
			})
			if err != nil {
				t.Fatalf("runQuery returned error: %v", err)
			}
		})

		if !strings.Contains(output, "P95 CPU utilisation for ocid1.instance: 12.50%") {
			t.Fatalf("unexpected log output: %q", output)
		}

		client.mu.Lock()
		defer client.mu.Unlock()

		if client.callCount != 1 {
			t.Fatalf("expected one call, got %d", client.callCount)
		}

		if client.lastArgs[0] != "ocid1.instance" || client.lastArgs[1] != true {
			t.Fatalf("unexpected arguments: %#v", client.lastArgs)
		}
	})
}

func TestRunQueryAllowsEmptyResults(t *testing.T) {
	t.Parallel()

	client := &fakeMetricsClient{ //nolint:exhaustruct
		err: oci.ErrNoMetricsData,
	}

	withMetricsClient(t, client, func() {
		output := captureLogs(t, func() {
			err := runQuery(queryConfig{
				instanceID:    "ocid1.instance",
				compartmentID: "ocid1.compartment",
				region:        "",
				last7d:        true,
				timeout:       defaultTimeout,
				allowEmpty:    true,
			})
			if err != nil {
				t.Fatalf("runQuery returned error: %v", err)
			}
		})

		if !strings.Contains(output, "no metrics returned for ocid1.instance") {
			t.Fatalf("expected allow-empty log, got %q", output)
		}
	})
}

func TestRunQueryWrapsQueryErrors(t *testing.T) {
	t.Parallel()

	client := &fakeMetricsClient{ //nolint:exhaustruct
		err: errQueryFailure,
	}

	withMetricsClient(t, client, func() {
		err := runQuery(queryConfig{
			instanceID:    "ocid1.instance",
			compartmentID: "ocid1.compartment",
			region:        "",
			last7d:        true,
			timeout:       defaultTimeout,
			allowEmpty:    false,
		})
		if err == nil || !strings.Contains(err.Error(), "query P95 CPU: boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})
}

func TestRunQueryWrapsClientErrors(t *testing.T) {
	t.Parallel()

	metricsClientMutex.Lock()

	previousFactory := newMetricsClient
	newMetricsClient = func(string, string) (metricsQuerier, error) {
		return nil, errFactoryFailure
	}

	defer func() {
		newMetricsClient = previousFactory

		metricsClientMutex.Unlock()
	}()

	err := runQuery(queryConfig{
		instanceID:    "ocid1.instance",
		compartmentID: "ocid1.compartment",
		region:        "",
		last7d:        true,
		timeout:       defaultTimeout,
		allowEmpty:    false,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "build instance principal client: factory failure") {
		t.Fatalf("expected client factory error, got %v", err)
	}
}
