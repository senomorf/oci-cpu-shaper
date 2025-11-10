package est

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Observation represents a host CPU utilisation snapshot derived from /proc/stat
// deltas. The Utilisation field is expressed as a ratio in the range [0,1].
type Observation struct {
	Timestamp    time.Time
	Utilisation  float64
	BusyJiffies  uint64
	TotalJiffies uint64
	Err          error
}

// Source describes an entity capable of returning cumulative CPU jiffy counters.
type Source interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures the cumulative idle and total jiffy counters at a point in time.
type Snapshot struct {
	Idle  uint64
	Total uint64
}

// FileSource reads CPU statistics from the Linux /proc/stat pseudo file.
type FileSource struct {
	Path string
}

// Snapshot implements the Source interface.
func (f FileSource) Snapshot(ctx context.Context) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	default:
	}

	path := f.Path
	if path == "" {
		path = "/proc/stat"
	}

	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer file.Close()

	snap, err := parseCPUStat(file)
	if err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// Sampler periodically samples CPU statistics and publishes utilisation observations.
type Sampler struct {
	source   Source
	interval time.Duration
	now      func() time.Time
	started  atomic.Bool
}

// DefaultInterval is used when a zero or negative interval is supplied.
const DefaultInterval = time.Second

// NewSampler constructs a Sampler using the provided Source and interval.
func NewSampler(src Source, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Sampler{
		source:   src,
		interval: interval,
		now:      time.Now,
	}
}

// Run begins sampling until the supplied context is cancelled. Observations are
// delivered on the returned channel which is closed on exit.
func (s *Sampler) Run(ctx context.Context) <-chan Observation {
	ch := make(chan Observation, 1)
	if !s.started.CompareAndSwap(false, true) {
		ch <- Observation{Err: errors.New("sampler already started")}
		close(ch)
		return ch
	}

	go func() {
		defer close(ch)

		src := s.source
		if src == nil {
			src = FileSource{}
		}

		last, err := src.Snapshot(ctx)
		if err != nil {
			select {
			case ch <- Observation{Timestamp: s.now(), Err: err}:
			case <-ctx.Done():
			}
			return
		}

		nowFn := s.now
		if nowFn == nil {
			nowFn = time.Now
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snap, err := src.Snapshot(ctx)
				if err != nil {
					select {
					case ch <- Observation{Timestamp: nowFn(), Err: err}:
					case <-ctx.Done():
					}
					continue
				}

				obs := buildObservation(nowFn(), last, snap)
				last = snap

				select {
				case ch <- obs:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch
}

func buildObservation(ts time.Time, previous, current Snapshot) Observation {
	totalDelta := diffCounter(previous.Total, current.Total)
	idleDelta := diffCounter(previous.Idle, current.Idle)
	busyDelta := uint64(0)
	utilisation := 0.0

	if totalDelta > 0 && idleDelta <= totalDelta {
		busyDelta = totalDelta - idleDelta
		utilisation = float64(busyDelta) / float64(totalDelta)
		if utilisation < 0 {
			utilisation = 0
		} else if utilisation > 1 {
			utilisation = 1
		}
	}

	return Observation{
		Timestamp:    ts,
		Utilisation:  utilisation,
		BusyJiffies:  busyDelta,
		TotalJiffies: totalDelta,
	}
}

func diffCounter(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	// Counter wrapped; reset to zero delta.
	return 0
}

func parseCPUStat(r io.Reader) (Snapshot, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{}, io.EOF
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return Snapshot{}, fmt.Errorf("unexpected /proc/stat format: %q", line)
	}

	fields := strings.Fields(line)
	if len(fields) < 5 {
		return Snapshot{}, fmt.Errorf("/proc/stat cpu line too short: %q", line)
	}

	var total uint64
	var idle uint64
	for i, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse field %d: %w", i+1, err)
		}
		total += value
		if i == 3 { // idle
			idle += value
		}
		if i == 4 { // iowait counted as idle per kernel docs
			idle += value
		}
	}

	return Snapshot{Idle: idle, Total: total}, nil
}
