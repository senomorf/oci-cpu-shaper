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
	err := ctx.Err()
	if err != nil {
		return Snapshot{}, fmt.Errorf("file source context: %w", err)
	}

	path := f.Path
	if path == "" {
		path = "/proc/stat"
	}

	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open %s: %w", path, err)
	}

	snap, parseErr := parseCPUStat(file)
	closeErr := file.Close()

	if parseErr != nil {
		return Snapshot{}, fmt.Errorf("parse %s: %w", path, parseErr)
	}

	if closeErr != nil {
		return Snapshot{}, fmt.Errorf("close %s: %w", path, closeErr)
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

const (
	minimumCPUFields = 5
	idleFieldIndex   = 3
	ioWaitFieldIndex = 4
)

var (
	ErrSamplerAlreadyStarted    = errors.New("est: sampler already started")
	ErrUnexpectedProcStatFormat = errors.New("est: unexpected /proc/stat format")
	ErrProcStatTooShort         = errors.New("est: /proc/stat cpu line too short")
)

// NewSampler constructs a Sampler using the provided Source and interval.
func NewSampler(src Source, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = DefaultInterval
	}

	sampler := new(Sampler)
	sampler.source = src
	sampler.interval = interval
	sampler.now = time.Now

	return sampler
}

// Run begins sampling until the supplied context is cancelled. Observations are
// delivered on the returned channel which is closed on exit.
func (s *Sampler) Run(ctx context.Context) <-chan Observation {
	observations := make(chan Observation, 1)

	if !s.started.CompareAndSwap(false, true) {
		s.publishError(ctx, observations, ErrSamplerAlreadyStarted)
		close(observations)

		return observations
	}

	go s.startSampling(ctx, observations)

	return observations
}

func (s *Sampler) startSampling(ctx context.Context, observations chan<- Observation) {
	defer close(observations)

	src := s.source
	if src == nil {
		src = FileSource{Path: ""}
	}

	last, err := src.Snapshot(ctx)
	if err != nil {
		s.publishError(ctx, observations, fmt.Errorf("initial snapshot: %w", err))

		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.sampleLoop(ctx, src, last, ticker, observations)
}

func (s *Sampler) sampleLoop(
	ctx context.Context,
	src Source,
	last Snapshot,
	ticker *time.Ticker,
	observations chan<- Observation,
) {
	nowFn := s.timeSource()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap, err := src.Snapshot(ctx)
			if err != nil {
				s.publishError(ctx, observations, fmt.Errorf("sample snapshot: %w", err))

				continue
			}

			obs := buildObservation(nowFn(), last, snap)
			last = snap

			if !s.publishObservation(ctx, observations, obs) {
				return
			}
		}
	}
}

func (s *Sampler) publishError(ctx context.Context, observations chan<- Observation, err error) {
	observation := Observation{
		Timestamp:    s.timeSource()(),
		Utilisation:  0,
		BusyJiffies:  0,
		TotalJiffies: 0,
		Err:          err,
	}

	s.publishObservation(ctx, observations, observation)
}

func (s *Sampler) publishObservation(
	ctx context.Context,
	observations chan<- Observation,
	observation Observation,
) bool {
	select {
	case observations <- observation:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Sampler) timeSource() func() time.Time {
	if s.now != nil {
		return s.now
	}

	return time.Now
}

func buildObservation(timestamp time.Time, previous, current Snapshot) Observation {
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
		Timestamp:    timestamp,
		Utilisation:  utilisation,
		BusyJiffies:  busyDelta,
		TotalJiffies: totalDelta,
		Err:          nil,
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
		err := scanner.Err()
		if err != nil {
			return Snapshot{}, fmt.Errorf("scan cpu line: %w", err)
		}

		return Snapshot{}, io.EOF
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrUnexpectedProcStatFormat, line)
	}

	fields := strings.Fields(line)
	if len(fields) < minimumCPUFields {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrProcStatTooShort, line)
	}

	var (
		total uint64
		idle  uint64
	)

	for index, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse field %d: %w", index+1, err)
		}

		total += value
		if index == idleFieldIndex {
			idle += value
		}

		if index == ioWaitFieldIndex {
			idle += value
		}
	}

	return Snapshot{Idle: idle, Total: total}, nil
}
