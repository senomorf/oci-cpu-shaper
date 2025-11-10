package shape

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync/atomic"
	"time"
)

// Pool drives a group of duty-cycle workers that consume CPU in short quanta.
type Pool struct {
	workers int
	quantum time.Duration

	busyFunc  func(time.Duration)
	sleepFunc func(time.Duration)
	yieldFunc func()

	targetBits atomic.Uint64
}

// DefaultQuantum bounds the busy loop to a responsive interval.
const DefaultQuantum = time.Millisecond

// NewPool constructs a worker pool with the provided worker count and quantum duration.
func NewPool(workers int, quantum time.Duration) (*Pool, error) {
	if workers <= 0 {
		return nil, errors.New("worker count must be positive")
	}
	if quantum <= 0 {
		quantum = DefaultQuantum
	}
	if quantum < time.Millisecond {
		quantum = time.Millisecond
	}
	if quantum > 5*time.Millisecond {
		quantum = 5 * time.Millisecond
	}

	p := &Pool{
		workers:   workers,
		quantum:   quantum,
		busyFunc:  busyWait,
		sleepFunc: time.Sleep,
		yieldFunc: runtime.Gosched,
	}
	p.SetTarget(0)
	return p, nil
}

// Start launches the worker goroutines. The pool terminates when the context is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		go p.worker(ctx)
	}
}

// SetTarget updates the duty cycle target in the range [0,1].
func (p *Pool) SetTarget(target float64) {
	if math.IsNaN(target) {
		target = 0
	}
	if target < 0 {
		target = 0
	} else if target > 1 {
		target = 1
	}
	p.targetBits.Store(math.Float64bits(target))
}

// Target returns the current duty-cycle target.
func (p *Pool) Target() float64 {
	return math.Float64frombits(p.targetBits.Load())
}

func (p *Pool) worker(ctx context.Context) {
	quantum := p.quantum
	busyFn := p.busyFunc
	sleepFn := p.sleepFunc
	yieldFn := p.yieldFunc

	ticker := time.NewTicker(quantum)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			target := p.Target()
			busyDuration := time.Duration(target * float64(quantum))
			if busyDuration > quantum {
				busyDuration = quantum
			}
			idleDuration := quantum - busyDuration

			if busyDuration > 0 {
				busyFn(busyDuration)
			} else {
				yieldFn()
			}

			if idleDuration > 0 {
				sleepFn(idleDuration)
			} else {
				yieldFn()
			}

			yieldFn()
		}
	}
}

func busyWait(duration time.Duration) {
	if duration <= 0 {
		return
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		runtime.Gosched()
	}
}
