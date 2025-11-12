//go:build load

//nolint:testpackage // load harness needs access to internal hooks
package shape

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type processMetrics struct {
	cpuSeconds   float64
	rssBytes     uint64
	rssPeakBytes uint64
}

func captureProcessMetrics(t testing.TB) processMetrics {
	t.Helper()

	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("getrusage: %v", err)
	}

	cpuSeconds := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1_000_000
	cpuSeconds += float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1_000_000

	rssBytes := readCurrentRSS(t)
	rssPeakBytes := uint64(usage.Maxrss) * 1024 // Linux reports max RSS in KiB

	return processMetrics{
		cpuSeconds:   cpuSeconds,
		rssBytes:     rssBytes,
		rssPeakBytes: rssPeakBytes,
	}
}

func readCurrentRSS(t testing.TB) uint64 {
	t.Helper()

	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read /proc/self/status: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("unexpected VmRSS format: %q", line)
		}

		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			t.Fatalf("parse VmRSS: %v", parseErr)
		}

		return value * 1024 // values reported in KiB
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan VmRSS: %v", err)
	}

	t.Fatalf("VmRSS not found in /proc/self/status")

	return 0
}

func TestPoolLoad24hEquivalent(t *testing.T) {
	const (
		workerCount = 2
		quantum     = 5 * time.Millisecond
		dutyTarget  = 0.33
		loadWindow  = 24 * time.Hour
		cpuBudget   = 0.002 // 0.2% of a single core (§10)
		rssBudget   = 15 * 1024 * 1024
	)

	ticksPerTicker := int64(loadWindow / quantum)
	if loadWindow%quantum != 0 {
		ticksPerTicker++
	}

	if ticksPerTicker <= 0 {
		ticksPerTicker = 1
	}

	equivalentRuntime := time.Duration(ticksPerTicker) * quantum

	scheduler := newDeterministicScheduler(ticksPerTicker, workerCount)

	pool, err := NewPool(workerCount, quantum)
	if err != nil {
		t.Fatalf("unexpected error constructing pool: %v", err)
	}

	pool.tickerFactory = scheduler.newTicker

	var (
		busyTotal  atomic.Int64
		idleTotal  atomic.Int64
		yieldCount atomic.Int64
	)

	pool.busyFunc = func(duration time.Duration) {
		busyTotal.Add(int64(duration))
	}
	pool.sleepFunc = func(duration time.Duration) {
		idleTotal.Add(int64(duration))
	}
	pool.yieldFunc = func() {
		yieldCount.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startMetrics := captureProcessMetrics(t)
	wallStart := time.Now()

	pool.Start(ctx)
	pool.SetTarget(dutyTarget)

	<-scheduler.Ready()

	schedulerDone := make(chan struct{})

	go func() {
		scheduler.Wait()
		cancel()
		close(schedulerDone)
	}()

	<-schedulerDone

	time.Sleep(10 * time.Millisecond)

	endMetrics := captureProcessMetrics(t)
	wallElapsed := time.Since(wallStart)

	totalBusy := time.Duration(busyTotal.Load())
	totalIdle := time.Duration(idleTotal.Load())
	totalTicks := scheduler.TotalTicks()
	expectedTicks := ticksPerTicker * int64(workerCount)

	if totalTicks != expectedTicks {
		t.Fatalf("expected %d ticks across workers, captured %d", expectedTicks, totalTicks)
	}

	expectedTimeline := equivalentRuntime * time.Duration(workerCount)
	accounted := totalBusy + totalIdle

	if accounted != expectedTimeline {
		t.Fatalf("expected accounting %v, busy %v idle %v", expectedTimeline, totalBusy, totalIdle)
	}

	busyRatio := float64(totalBusy) / float64(expectedTimeline)
	if math.Abs(busyRatio-dutyTarget) > 0.02 {
		t.Fatalf("duty-cycle drift: expected %.2f observed %.4f", dutyTarget, busyRatio)
	}

	cpuDelta := endMetrics.cpuSeconds - startMetrics.cpuSeconds
	cpuShare := cpuDelta / equivalentRuntime.Seconds()

	if cpuShare > cpuBudget {
		t.Fatalf("cpu usage %.6f exceeds budget %.6f", cpuShare, cpuBudget)
	}

	maxRSS := endMetrics.rssBytes
	if startMetrics.rssBytes > maxRSS {
		maxRSS = startMetrics.rssBytes
	}

	if maxRSS > rssBudget {
		t.Fatalf("rss %d exceeds budget %d", maxRSS, rssBudget)
	}

	logDir := filepath.Join(repoRoot(t), "artifacts", "load")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("creating log directory: %v", err)
	}

	logPath := filepath.Join(logDir, "pool-24h.log")
	logBody := fmt.Sprintf(
		"workers=%d\nquantum=%s\nticks_per_worker=%d\nworkers_total_ticks=%d\nequivalent_runtime=%s\nduty_target=%.2f\n"+
			"busy_ratio=%.6f\nbusy_total_ns=%d\nidle_total_ns=%d\ncpu_seconds_start=%.6f\n"+
			"cpu_seconds_end=%.6f\ncpu_seconds_delta=%.6f\ncpu_share=%.6f\nrss_start_bytes=%d\n"+
			"rss_end_bytes=%d\nrss_peak_start_bytes=%d\nrss_peak_end_bytes=%d\nrss_peak_bytes=%d\n"+
			"yields=%d\nwall_elapsed=%s\n",
		workerCount,
		quantum,
		ticksPerTicker,
		totalTicks,
		equivalentRuntime,
		dutyTarget,
		busyRatio,
		totalBusy.Nanoseconds(),
		totalIdle.Nanoseconds(),
		startMetrics.cpuSeconds,
		endMetrics.cpuSeconds,
		cpuDelta,
		cpuShare,
		startMetrics.rssBytes,
		endMetrics.rssBytes,
		startMetrics.rssPeakBytes,
		endMetrics.rssPeakBytes,
		maxRSS,
		yieldCount.Load(),
		wallElapsed,
	)

	if err := os.WriteFile(logPath, []byte(logBody), 0o644); err != nil {
		t.Fatalf("writing load log: %v", err)
	}

	t.Logf("24h-equivalent load metrics written to %s", logPath)
}

type deterministicScheduler struct {
	ticksPerTicker  int64
	expectedTickers int64
	totalTicks      atomic.Int64
	registered      atomic.Int64
	wg              sync.WaitGroup
	ready           chan struct{}
	readyOnce       sync.Once
}

func newDeterministicScheduler(ticksPerTicker int64, expectedTickers int) *deterministicScheduler {
	scheduler := &deterministicScheduler{
		ticksPerTicker:  ticksPerTicker,
		expectedTickers: int64(expectedTickers),
		ready:           make(chan struct{}),
	}

	if expectedTickers == 0 {
		scheduler.readyOnce.Do(func() {
			close(scheduler.ready)
		})
	}

	return scheduler
}

func (s *deterministicScheduler) newTicker(_ time.Duration) ticker {
	manual := &manualTicker{
		scheduler: s,
		remaining: s.ticksPerTicker,
		ch:        make(chan time.Time),
		stopCh:    make(chan struct{}),
	}

	s.wg.Add(1)
	registered := s.registered.Add(1)
	if s.expectedTickers > 0 && registered == s.expectedTickers {
		s.readyOnce.Do(func() {
			close(s.ready)
		})
	}
	go manual.run()

	return manual
}

func (s *deterministicScheduler) Ready() <-chan struct{} {
	return s.ready
}

func (s *deterministicScheduler) Wait() {
	s.wg.Wait()
}

func (s *deterministicScheduler) TotalTicks() int64 {
	return s.totalTicks.Load()
}

func (s *deterministicScheduler) record(sent int64) {
	s.totalTicks.Add(sent)
}

type manualTicker struct {
	scheduler *deterministicScheduler
	remaining int64
	ch        chan time.Time
	stopCh    chan struct{}

	stopOnce sync.Once
}

func (t *manualTicker) C() <-chan time.Time {
	return t.ch
}

func (t *manualTicker) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
}

func (t *manualTicker) run() {
	defer func() {
		t.scheduler.wg.Done()
	}()

	var sent int64

	for sent < t.remaining {
		select {
		case <-t.stopCh:
			t.scheduler.record(sent)
			return
		case t.ch <- time.Time{}:
			sent++
		}
	}

	t.scheduler.record(sent)
}

func repoRoot(t testing.TB) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("determine working directory: %v", err)
	}

	dir := wd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("go.mod not found from %s", wd)
		}

		dir = next
	}
}
