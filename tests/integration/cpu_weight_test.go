//go:build integration

// Package integration exercises container-level responsiveness guarantees.
package integration

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	integrationImageTag = "oci-cpu-shaper:integration-rootful"
	hogCmdImportPath    = "./tests/integration/cmd/cpu-hog"
)

func TestCPUWeightResponsiveness(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("integration test requires a Linux host")
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI not available: %v", err)
	}

	ensureCgroupV2(t)

	repoRoot := repositoryRoot(t)
	hogBinary := buildHogBinary(t, repoRoot)
	buildIntegrationImage(t, repoRoot)

	highWeightName := containerName("cpu-weight-high")
	lowWeightName := containerName("cpu-weight-low")

	runContainer(t, containerConfig{
		name:       highWeightName,
		image:      "alpine:3.20",
		cpuShares:  1024,
		hogBinary:  hogBinary,
		duration:   45 * time.Second,
		cpuWorkers: 1,
	})
	runContainer(t, containerConfig{
		name:       lowWeightName,
		image:      integrationImageTag,
		cpuShares:  2,
		hogBinary:  hogBinary,
		duration:   45 * time.Second,
		cpuWorkers: 1,
	})

	time.Sleep(10 * time.Second)

	highWeightStats := readCPUStats(t, highWeightName)
	lowWeightStats := readCPUStats(t, lowWeightName)

	t.Logf("high-weight container usage: %d µs (weight=%d)", highWeightStats.usageMicros, highWeightStats.weight)
	t.Logf("low-weight container usage: %d µs (weight=%d)", lowWeightStats.usageMicros, lowWeightStats.weight)

	if highWeightStats.weight <= lowWeightStats.weight {
		t.Fatalf("expected high-weight container (%d) to exceed low-weight container (%d)", highWeightStats.weight, lowWeightStats.weight)
	}

	if lowWeightStats.usageMicros == 0 {
		t.Fatalf("low-weight container reported zero CPU usage; inspect docker logs for %s", lowWeightName)
	}

	usageRatio := float64(highWeightStats.usageMicros) / float64(lowWeightStats.usageMicros)
	t.Logf("observed CPU usage ratio (high/low): %.2f", usageRatio)

	const minimumExpectedRatio = 5.0
	if usageRatio < minimumExpectedRatio {
		t.Fatalf("expected high-weight container to receive at least %.1fx CPU time (got %.2fx)", minimumExpectedRatio, usageRatio)
	}
}

type containerConfig struct {
	name       string
	image      string
	cpuShares  int
	hogBinary  string
	duration   time.Duration
	cpuWorkers int
}

type cpuStats struct {
	usageMicros uint64
	weight      uint64
}

func ensureCgroupV2(t *testing.T) {
	t.Helper()

	controllersPath := "/sys/fs/cgroup/cgroup.controllers"
	data, err := os.ReadFile(controllersPath)
	if err != nil {
		t.Fatalf("cgroup v2 controllers file not readable (%s): %v", controllersPath, err)
	}

	if !strings.Contains(string(data), "cpu") {
		t.Fatalf("cgroup v2 cpu controller is unavailable; controllers=%q", strings.TrimSpace(string(data)))
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("determine working directory: %v", err)
	}

	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err = os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	return root
}

func buildHogBinary(t *testing.T, repoRoot string) string {
	t.Helper()

	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "cpu-hog")

	build := exec.Command("go", "build", "-o", binaryPath, hogCmdImportPath)
	build.Dir = repoRoot
	build.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+runtime.GOARCH,
	)

	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cpu hog helper: %v\n%s", err, output)
	}

	return binaryPath
}

func buildIntegrationImage(t *testing.T, repoRoot string) {
	t.Helper()

	cmd := exec.Command(
		"docker", "build",
		"--target", "rootful",
		"-t", integrationImageTag,
		"-f", filepath.Join("deploy", "Dockerfile"),
		".",
	)
	cmd.Dir = repoRoot

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build integration image: %v\n%s", err, output)
	}
}

func runContainer(t *testing.T, cfg containerConfig) {
	t.Helper()

	args := []string{
		"run",
		"--detach",
		"--name", cfg.name,
		"--cpuset-cpus=0",
		"--cpu-shares", strconv.Itoa(cfg.cpuShares),
		"-v", fmt.Sprintf("%s:/hog:ro", cfg.hogBinary),
		"--entrypoint", "/hog",
		cfg.image,
		fmt.Sprintf("-duration=%ds", int(cfg.duration.Seconds())),
		fmt.Sprintf("-workers=%d", cfg.cpuWorkers),
	}

	run := exec.Command("docker", args...)
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("start container %s: %v\n%s", cfg.name, err, output)
	}

	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", cfg.name).Run()
	})

	waitForRunning(t, cfg.name, 10*time.Second)
}

func waitForRunning(t *testing.T, name string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inspect := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name)
		output, err := inspect.CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == "true" {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("container %s did not report running state within %s", name, timeout)
}

func readCPUStats(t *testing.T, containerName string) cpuStats {
	t.Helper()

	pid := containerPID(t, containerName)
	cgroupPath := cgroupPathForPID(t, pid)

	statsPath := filepath.Join(cgroupPath, "cpu.stat")
	weightPath := filepath.Join(cgroupPath, "cpu.weight")

	usage, err := parseUsageMicros(statsPath)
	if err != nil {
		t.Fatalf("parse cpu.stat for %s: %v", containerName, err)
	}

	weight, err := parseWeight(weightPath)
	if err != nil {
		t.Fatalf("parse cpu.weight for %s: %v", containerName, err)
	}

	return cpuStats{
		usageMicros: usage,
		weight:      weight,
	}
}

func containerPID(t *testing.T, name string) int {
	t.Helper()

	inspect := exec.Command("docker", "inspect", "-f", "{{.State.Pid}}", name)
	output, err := inspect.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect container %s pid: %v\n%s", name, err, output)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("parse container %s pid: %v", name, err)
	}

	if pid <= 0 {
		t.Fatalf("container %s reported invalid pid %d", name, pid)
	}

	return pid
}

func cgroupPathForPID(t *testing.T, pid int) string {
	t.Helper()

	cgroupFile := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("read cgroup data for pid %d: %v", pid, err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			continue
		}

		if parts[0] == "0" {
			relPath := parts[2]
			if relPath == "" {
				break
			}

			absPath := filepath.Join("/sys/fs/cgroup", relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan cgroup entries for pid %d: %v", pid, err)
	}

	t.Fatalf("cgroup v2 path not found for pid %d", pid)

	return ""
}

func parseUsageMicros(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "usage_usec" {
			value, convErr := strconv.ParseUint(fields[1], 10, 64)
			if convErr != nil {
				return 0, convErr
			}

			return value, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, errors.New("usage_usec not present in cpu.stat")
}

func parseWeight(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, errors.New("cpu.weight empty")
	}

	return strconv.ParseUint(trimmed, 10, 64)
}

func containerName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
