package est

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	snapshots []Snapshot
	err       error
	index     int
}

func (f *fakeSource) Snapshot(ctx context.Context) (Snapshot, error) {
	if f.err != nil {
		return Snapshot{}, f.err
	}
	if f.index >= len(f.snapshots) {
		// Return the last snapshot repeatedly once exhausted.
		return f.snapshots[len(f.snapshots)-1], nil
	}
	snap := f.snapshots[f.index]
	f.index++
	return snap, nil
}

func TestSamplerEmitsObservations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &fakeSource{snapshots: []Snapshot{
		{Idle: 10, Total: 20},
		{Idle: 12, Total: 30},
		{Idle: 13, Total: 40},
	}}

	sampler := NewSampler(src, time.Millisecond)
	sampler.now = func() time.Time { return time.Unix(0, 0) }

	obsCh := sampler.Run(ctx)

	var obs []Observation
	timeout := time.After(100 * time.Millisecond)
	for len(obs) < 2 {
		select {
		case o, ok := <-obsCh:
			if !ok {
				t.Fatalf("channel closed prematurely; collected %d observations", len(obs))
			}
			if o.Err != nil {
				t.Fatalf("unexpected error: %v", o.Err)
			}
			obs = append(obs, o)
		case <-timeout:
			t.Fatalf("timed out waiting for observations; collected %d", len(obs))
		}
	}

	cancel()

	if got, want := len(obs), 2; got != want {
		t.Fatalf("expected %d observations, got %d", want, got)
	}

	if got, want := obs[0].Utilisation, 0.8; math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected utilisation: got %.2f want %.2f", got, want)
	}
	if got, want := obs[0].BusyJiffies, uint64(8); got != want {
		t.Fatalf("unexpected busy jiffies: got %d want %d", got, want)
	}
	if got, want := obs[0].TotalJiffies, uint64(10); got != want {
		t.Fatalf("unexpected total jiffies: got %d want %d", got, want)
	}

	if got, want := obs[1].Utilisation, 0.9; math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected utilisation: got %.2f want %.2f", got, want)
	}
}

func TestBuildObservationHandlesDiverseDeltas(t *testing.T) {
	cases := []struct {
		name    string
		prev    Snapshot
		current Snapshot
		util    float64
		busy    uint64
		total   uint64
	}{
		{
			name:    "no-change",
			prev:    Snapshot{Idle: 10, Total: 20},
			current: Snapshot{Idle: 10, Total: 20},
			util:    0,
			busy:    0,
			total:   0,
		},
		{
			name:    "full-busy",
			prev:    Snapshot{Idle: 10, Total: 20},
			current: Snapshot{Idle: 10, Total: 40},
			util:    1,
			busy:    20,
			total:   20,
		},
		{
			name:    "wrap-around",
			prev:    Snapshot{Idle: 100, Total: 200},
			current: Snapshot{Idle: 10, Total: 20},
			util:    0,
			busy:    0,
			total:   0,
		},
		{
			name:    "partial-busy",
			prev:    Snapshot{Idle: 40, Total: 100},
			current: Snapshot{Idle: 50, Total: 140},
			util:    0.75,
			busy:    30,
			total:   40,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := buildObservation(time.Unix(0, 0), tc.prev, tc.current)
			if math.Abs(obs.Utilisation-tc.util) > 1e-9 {
				t.Fatalf("unexpected utilisation: got %.2f want %.2f", obs.Utilisation, tc.util)
			}
			if obs.BusyJiffies != tc.busy {
				t.Fatalf("unexpected busy: got %d want %d", obs.BusyJiffies, tc.busy)
			}
			if obs.TotalJiffies != tc.total {
				t.Fatalf("unexpected total: got %d want %d", obs.TotalJiffies, tc.total)
			}
		})
	}
}

func TestParseCPUStat(t *testing.T) {
	stat := "cpu  1 2 3 4 5 6 7 8 9 10\ncpu0 1 2 3 4 5 6 7 8 9 10\n"
	snap, err := parseCPUStat(strings.NewReader(stat))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Total == 0 {
		t.Fatalf("expected total to be non-zero")
	}
	if snap.Idle != 9 { // idle + iowait
		t.Fatalf("unexpected idle: got %d want 9", snap.Idle)
	}
}
