//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"oci-cpu-shaper/internal/e2eclient"
	"oci-cpu-shaper/pkg/imds"
	interne2e "oci-cpu-shaper/tests/internal/e2e"
)

type logEntry map[string]any

func TestCLIEmulationOfflineAndOnline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoRoot := interne2e.RepositoryRoot(t)
	binary := interne2e.BuildShaperBinary(t, repoRoot, "e2e")

	offlineIMDS := interne2e.StartIMDSServer(t, interne2e.IMDSConfig{
		Region:          "us-test-1",
		CanonicalRegion: "us-test-1",
		InstanceID:      "ocid1.instance.oc1..offline",
		CompartmentID:   "ocid1.compartment.oc1..offline",
		Shape:           imds.ShapeConfig{OCPUs: 2, MemoryInGBs: 32},
	})
	offlineMonitoring := interne2e.StartMonitoringServer(t, []interne2e.MonitoringResponse{{Value: 0.24}})

	offlineMetricsPort := interne2e.FreePort(t)
	offlineConfig := writeConfig(t, "offline.yaml", fmt.Sprintf(`
controller:
  interval: 1s
  relaxedInterval: 2s
estimator:
  interval: 200ms
pool:
  workers: 1
  quantum: 150ms
http:
  bind: "127.0.0.1:%d"
oci:
  instanceId: "ocid1.instance.oc1..offline"
  offline: true
`, offlineMetricsPort))

	offlineLogs, offlineMetrics := runShaper(ctx, t, binary, offlineConfig, offlineMetricsPort, map[string]string{
		"OCI_CPU_SHAPER_IMDS_ENDPOINT":  offlineIMDS.Endpoint(),
		e2eclient.MonitoringEndpointEnv: offlineMonitoring.URL(),
	})

	if requests := offlineIMDS.Requests(); len(requests) != 0 {
		t.Fatalf("expected offline mode to skip IMDS lookups, saw %d", len(requests))
	}

	if requests := offlineMonitoring.Requests(); len(requests) != 0 {
		t.Fatalf("expected offline mode to skip Monitoring queries, saw %d", len(requests))
	}

	assertMetricsState(t, offlineMetrics, "normal")
	requireTransition(t, offlineLogs, "", "fallback")
	requireTransition(t, offlineLogs, "fallback", "normal")
	assertOfflineLog(t, offlineLogs, true)

	onlineIMDS := interne2e.StartIMDSServer(t, interne2e.IMDSConfig{
		Region:          "us-test-1",
		CanonicalRegion: "us-test-1",
		InstanceID:      "ocid1.instance.oc1..example",
		CompartmentID:   "ocid1.compartment.oc1..example",
		Shape:           imds.ShapeConfig{OCPUs: 4, MemoryInGBs: 64},
	})
	onlineMonitoring := interne2e.StartMonitoringServer(t, []interne2e.MonitoringResponse{
		{Status: 503, Body: "service unavailable"},
		{Value: 0.28},
	})

	onlineMetricsPort := interne2e.FreePort(t)
	onlineConfig := writeConfig(t, "online.yaml", fmt.Sprintf(`
controller:
  interval: 1s
  relaxedInterval: 2s
estimator:
  interval: 200ms
pool:
  workers: 1
  quantum: 150ms
http:
  bind: "127.0.0.1:%d"
oci:
  fallbackTarget: 0.25
`, onlineMetricsPort))

	onlineLogs, onlineMetrics := runShaper(ctx, t, binary, onlineConfig, onlineMetricsPort, map[string]string{
		"OCI_CPU_SHAPER_IMDS_ENDPOINT":  onlineIMDS.Endpoint(),
		e2eclient.MonitoringEndpointEnv: onlineMonitoring.URL(),
	})

	imdsRequests := onlineIMDS.Requests()
	if len(imdsRequests) == 0 {
		t.Fatal("expected online mode to contact IMDS")
	}

	requirePathObserved(t, imdsRequests, "/opc/v2/region")
	requirePathObserved(t, imdsRequests, "/opc/v2/compartmentId")

	monitoringRequests := onlineMonitoring.Requests()
	if len(monitoringRequests) < 1 {
		t.Fatalf("expected monitoring requests, saw %d", len(monitoringRequests))
	}

	assertMetricsState(t, onlineMetrics, "normal")
	requireTransition(t, onlineLogs, "", "fallback")
	requireTransition(t, onlineLogs, "fallback", "normal")
	assertOfflineLog(t, onlineLogs, false)
}

func runShaper(
	ctx context.Context,
	t *testing.T,
	binary string,
	configPath string,
	metricsPort int,
	env map[string]string,
) ([]logEntry, []byte) {
	t.Helper()

	var output bytes.Buffer

	cmd := exec.CommandContext(ctx, binary, "--config", configPath, "--shutdown-after=4s", "--log-level", "debug")
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start shaper: %v", err)
	}

	metricsURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsPort)
	var metricsData []byte
	deadline := time.Now().Add(2500 * time.Millisecond)
	for {
		snapshot, err := interne2e.WaitForMetrics(ctx, metricsURL)
		if err != nil {
			t.Fatalf("wait for metrics: %v", err)
		}

		metricsData = snapshot

		if time.Now().After(deadline) {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("shaper exited with error: %v\n%s", err, output.String())
	}

	entries := parseLogEntries(t, output.Bytes())

	return entries, metricsData
}

func writeConfig(t *testing.T, name, contents string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func parseLogEntries(t *testing.T, data []byte) []logEntry {
	t.Helper()

	var entries []logEntry
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		var entry logEntry
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			t.Fatalf("unmarshal log line %q: %v", trimmed, err)
		}

		entries = append(entries, entry)
	}

	return entries
}

func assertMetricsState(t *testing.T, metrics []byte, expected string) {
	t.Helper()

	want := fmt.Sprintf(`shaper_state{state="%s"} 1`, expected)
	if !bytes.Contains(metrics, []byte(want)) {
		t.Fatalf("expected metrics to include %q\nmetrics:\n%s", want, metrics)
	}
}

func requireTransition(t *testing.T, logs []logEntry, from, to string) {
	t.Helper()

	for _, entry := range logs {
		message, _ := entry["message"].(string)
		if message != "controller state transition" {
			continue
		}

		prev, _ := entry["from"].(string)
		next, _ := entry["to"].(string)
		if prev == from && next == to {
			return
		}
	}

	t.Fatalf("expected transition from %q to %q not found", from, to)
}

func assertOfflineLog(t *testing.T, logs []logEntry, offline bool) {
	t.Helper()

	for _, entry := range logs {
		message, _ := entry["message"].(string)
		if message != "initialized subsystems" {
			continue
		}

		value, ok := entry["offline"].(bool)
		if !ok {
			t.Fatalf("initialized subsystems log missing offline field: %+v", entry)
		}

		if value != offline {
			t.Fatalf("expected offline=%v, got %v", offline, value)
		}

		return
	}

	t.Fatal("expected initialized subsystems log")
}

func requirePathObserved(t *testing.T, requests []string, expected string) {
	t.Helper()

	for _, path := range requests {
		if path == expected {
			return
		}
	}

	t.Fatalf("expected path %q in IMDS requests: %v", expected, requests)
}
