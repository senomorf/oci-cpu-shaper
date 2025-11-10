//nolint:testpackage // tests exercise internal helpers for coverage
package est

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errTestBoom = errors.New("test: boom")

type fakeSource struct {
	snapshots []Snapshot
	err       error
	index     int
}

func (f *fakeSource) Snapshot(_ context.Context) (Snapshot, error) {
	if f.err != nil {
		return Snapshot{}, f.err
	}

	if f.index >= len(f.snapshots) {
		if len(f.snapshots) == 0 {
			return Snapshot{Idle: 0, Total: 0}, nil
		}

		return f.snapshots[len(f.snapshots)-1], nil
	}

	snap := f.snapshots[f.index]
	f.index++

	return snap, nil
}

func TestSamplerEmitsObservations(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := &fakeSource{snapshots: []Snapshot{
		{Idle: 10, Total: 20},
		{Idle: 12, Total: 30},
		{Idle: 13, Total: 40},
	}, err: nil, index: 0}

	sampler := NewSampler(source, time.Millisecond)
	sampler.now = func() time.Time { return time.Unix(0, 0) }

	observations := gatherObservations(t, sampler.Run(ctx), 2)

	cancel()

	const tolerance = 1e-9

	if diff := math.Abs(observations[0].Utilisation - 0.8); diff > tolerance {
		t.Fatalf("unexpected utilisation: got %.2f want %.2f", observations[0].Utilisation, 0.8)
	}

	if observations[0].BusyJiffies != 8 {
		t.Fatalf("unexpected busy jiffies: got %d want %d", observations[0].BusyJiffies, 8)
	}

	if observations[0].TotalJiffies != 10 {
		t.Fatalf("unexpected total jiffies: got %d want %d", observations[0].TotalJiffies, 10)
	}

	if diff := math.Abs(observations[1].Utilisation - 0.9); diff > tolerance {
		t.Fatalf("unexpected utilisation: got %.2f want %.2f", observations[1].Utilisation, 0.9)
	}
}

func gatherObservations(t *testing.T, observationsCh <-chan Observation, count int) []Observation {
	t.Helper()

	observations := make([]Observation, 0, count)
	timeout := time.After(100 * time.Millisecond)

	for len(observations) < count {
		select {
		case observation, ok := <-observationsCh:
			if !ok {
				t.Fatalf("channel closed prematurely; collected %d observations", len(observations))
			}

			if observation.Err != nil {
				t.Fatalf("unexpected error: %v", observation.Err)
			}

			observations = append(observations, observation)
		case <-timeout:
			t.Fatalf("timed out waiting for observations; collected %d", len(observations))
		}
	}

	return observations
}

func TestBuildObservationHandlesDiverseDeltas(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		previous    Snapshot
		current     Snapshot
		utilisation float64
		busy        uint64
		total       uint64
	}{
		{
			name:        "no-change",
			previous:    Snapshot{Idle: 10, Total: 20},
			current:     Snapshot{Idle: 10, Total: 20},
			utilisation: 0,
			busy:        0,
			total:       0,
		},
		{
			name:        "full-busy",
			previous:    Snapshot{Idle: 10, Total: 20},
			current:     Snapshot{Idle: 10, Total: 40},
			utilisation: 1,
			busy:        20,
			total:       20,
		},
		{
			name:        "wrap-around",
			previous:    Snapshot{Idle: 100, Total: 200},
			current:     Snapshot{Idle: 10, Total: 20},
			utilisation: 0,
			busy:        0,
			total:       0,
		},
		{
			name:        "partial-busy",
			previous:    Snapshot{Idle: 40, Total: 100},
			current:     Snapshot{Idle: 50, Total: 140},
			utilisation: 0.75,
			busy:        30,
			total:       40,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			observation := buildObservation(time.Unix(0, 0), testCase.previous, testCase.current)
			assertObservation(t, observation, testCase.utilisation, testCase.busy, testCase.total)
		})
	}
}

func assertObservation(t *testing.T, observation Observation, util float64, busy, total uint64) {
	t.Helper()

	if diff := math.Abs(observation.Utilisation - util); diff > 1e-9 {
		t.Fatalf("unexpected utilisation: got %.2f want %.2f", observation.Utilisation, util)
	}

	if observation.BusyJiffies != busy {
		t.Fatalf("unexpected busy: got %d want %d", observation.BusyJiffies, busy)
	}

	if observation.TotalJiffies != total {
		t.Fatalf("unexpected total: got %d want %d", observation.TotalJiffies, total)
	}
}

func TestParseCPUStat(t *testing.T) {
	t.Parallel()

	stat := "cpu  1 2 3 4 5 6 7 8 9 10\ncpu0 1 2 3 4 5 6 7 8 9 10\n"

	snapshot, err := parseCPUStat(strings.NewReader(stat))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snapshot.Total == 0 {
		t.Fatalf("expected total to be non-zero")
	}

	if snapshot.Idle != 9 {
		t.Fatalf("unexpected idle: got %d want 9", snapshot.Idle)
	}
}

func TestFileSourceSnapshotContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := FileSource{Path: filepath.Join(t.TempDir(), "ignored")}

	_, err := source.Snapshot(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestFileSourceSnapshotReadsProvidedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	statPath := filepath.Join(dir, "stat")

	contents := "cpu  1 2 3 4 5 6 7 8 9 10\n"

	err := os.WriteFile(statPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("write temp stat file: %v", err)
	}

	snap, err := (FileSource{Path: statPath}).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	if snap.Total == 0 {
		t.Fatalf("expected total to be recorded")
	}

	if snap.Idle == 0 {
		t.Fatalf("expected idle jiffies to be recorded")
	}
}

func TestSamplerRunInitialSnapshotError(t *testing.T) {
	t.Parallel()

	sampler := NewSampler(&fakeSource{snapshots: nil, err: errTestBoom, index: 0}, time.Millisecond)
	sampler.now = func() time.Time { return time.Unix(123, 0) }

	ctx := t.Context()

	observations := sampler.Run(ctx)

	observation, ok := <-observations
	if !ok {
		t.Fatalf("expected error observation")
	}

	if observation.Err == nil || !strings.Contains(observation.Err.Error(), "initial snapshot") {
		t.Fatalf("expected initial snapshot error, got %v", observation.Err)
	}

	if observation.Timestamp != time.Unix(123, 0) {
		t.Fatalf("unexpected timestamp: %v", observation.Timestamp)
	}

	if _, ok := <-observations; ok {
		t.Fatalf("expected channel to be closed after error observation")
	}
}

func TestSamplerRunRejectsDoubleStart(t *testing.T) {
	t.Parallel()

	sampler := NewSampler(
		&fakeSource{snapshots: []Snapshot{{Idle: 1, Total: 2}}, err: nil, index: 0},
		time.Hour,
	)
	sampler.now = func() time.Time { return time.Unix(0, 0) }

	ctx, cancel := context.WithCancel(context.Background())
	first := sampler.Run(ctx)

	cancel()

	for {
		_, ok := <-first
		if !ok {
			break
		}
	}

	second := sampler.Run(context.Background())

	observation, ok := <-second
	if !ok {
		t.Fatalf("expected error observation from second run")
	}

	if !errors.Is(observation.Err, ErrSamplerAlreadyStarted) {
		t.Fatalf("expected ErrSamplerAlreadyStarted, got %v", observation.Err)
	}

	if _, ok := <-second; ok {
		t.Fatalf("expected second channel to be closed")
	}
}
