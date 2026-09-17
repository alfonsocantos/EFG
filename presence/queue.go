package presence

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueFull = errors.New("event queue is full")
var ErrQueueStopped = errors.New("event queue is stopped")

type eventJob struct {
	ctx     context.Context
	request EventsRequest
	result  chan error
}

type Queue struct {
	mu              sync.Mutex // Protects closing the channel against concurrent admission.
	closed          bool
	cancel          context.CancelFunc
	done            chan struct{}
	wg              sync.WaitGroup
	ctx             context.Context
	jobs            chan eventJob
	workers         int
	busy            atomic.Int64
	events          atomic.Uint64
	eventsPerSecond atomic.Uint64
}

type Health struct {
	Status          string `json:"status"`
	EventsPerSecond uint64 `json:"events_per_second"`
	Queue           struct {
		Length   int `json:"length"`
		Capacity int `json:"capacity"`
	} `json:"queue"`
	Workers struct {
		Busy  int64 `json:"busy"`
		Total int   `json:"total"`
	} `json:"workers"`
}

// NewQueue starts workers that stop when ctx is canceled.
func NewQueue(ctx context.Context, size, workers int, process func(context.Context, EventsRequest) error) *Queue {
	if size <= 0 || workers <= 0 {
		panic("Queue size and workers must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	q := &Queue{ctx: ctx, cancel: cancel, jobs: make(chan eventJob, size), workers: workers, done: make(chan struct{})}
	q.wg.Add(workers)
	for range workers {
		go q.work(process)
	}
	go func() {
		q.wg.Wait()
		close(q.done)
	}()
	go q.measureEvents()
	return q
}

// Events rejects immediately when full, otherwise waits for persistence.
func (q *Queue) Events(ctx context.Context, request EventsRequest) error {
	q.mu.Lock()
	if q.closed || q.ctx.Err() != nil {
		q.mu.Unlock()
		return ErrQueueStopped
	}
	if err := ctx.Err(); err != nil {
		q.mu.Unlock()
		return err
	}
	job := eventJob{ctx: ctx, request: request, result: make(chan error, 1)}
	select {
	case q.jobs <- job:
	default:
		q.mu.Unlock()
		return ErrQueueFull
	}
	q.mu.Unlock()
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-q.ctx.Done():
		// A completed job keeps its result even if the last worker just exited.
		select {
		case err := <-job.result:
			return err
		default:
			return ErrQueueStopped
		}
	}
}

func (q *Queue) work(process func(context.Context, EventsRequest) error) {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case job, ok := <-q.jobs:
			if !ok {
				return
			}
			ctx, cancel := context.WithCancel(job.ctx)
			stop := context.AfterFunc(q.ctx, cancel)
			err := ctx.Err()
			if q.ctx.Err() != nil {
				err = ErrQueueStopped
			} else if err == nil {
				q.busy.Add(1)
				err = process(ctx, job.request)
				if err == nil {
					q.events.Add(uint64(len(job.request.Events)))
				}
				q.busy.Add(-1)
			}
			stop()
			cancel()
			// Buffered so a disconnected caller never blocks a worker.
			job.result <- err
		}
	}
}

// Health is an approximate snapshot; admission is decided by Events.
func (q *Queue) Health() Health {
	var h Health
	h.Status = "ok"
	h.EventsPerSecond = q.eventsPerSecond.Load()
	h.Queue.Length = len(q.jobs)
	h.Queue.Capacity = cap(q.jobs)
	h.Workers.Busy = q.busy.Load()
	h.Workers.Total = q.workers
	return h
}

// Shutdown stops admission and drains accepted jobs. On timeout it cancels
// in-flight processing; callers can retry batches that were not acknowledged.
func (q *Queue) Shutdown(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		q.cancel()
		return nil
	case <-ctx.Done():
		q.cancel()
		return ctx.Err()
	}
}

// Measure independently of health polling, so multiple dashboards see the same
// approximate rate. The hot path only performs one atomic addition per batch.
func (q *Queue) measureEvents() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	previous := time.Now()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.done:
			return
		case <-ticker.C:
			now := time.Now()
			q.sampleEvents(now.Sub(previous))
			previous = now
		}
	}
}

func (q *Queue) sampleEvents(elapsed time.Duration) {
	count := q.events.Swap(0)
	q.eventsPerSecond.Store(uint64(float64(count) / elapsed.Seconds()))
}
