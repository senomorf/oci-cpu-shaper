package main

import (
	"context"
	"flag"
	"math"
	"runtime"
	"sync"
	"time"
)

const (
	defaultRunDuration        = 30 * time.Second
	accumulatorResetThreshold = 1_000_000
)

func main() {
	duration := flag.Duration("duration", defaultRunDuration, "how long to run the CPU hog")
	workers := flag.Int("workers", runtime.NumCPU(), "number of busy loop workers to launch")

	flag.Parse()

	if *workers <= 0 {
		*workers = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	runtime.GOMAXPROCS(*workers)

	var workerGroup sync.WaitGroup
	workerGroup.Add(*workers)

	for i := 0; i < *workers; i++ {
		go func() {
			defer workerGroup.Done()

			var accumulator float64

			for {
				select {
				case <-ctx.Done():
					return
				default:
					accumulator += math.Sqrt(accumulator + 1)
					if accumulator > accumulatorResetThreshold {
						accumulator = 0
					}
				}
			}
		}()
	}

	<-ctx.Done()
	workerGroup.Wait()
}
