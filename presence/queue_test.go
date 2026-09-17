package presence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fulldump/biff"
)

func TestQueueCancellation(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	started := make(chan struct{})
	finished := make(chan struct{})
	queue := NewQueue(ctx, 1, 1, func(ctx context.Context, _ EventsRequest) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	})
	requestCtx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- queue.Events(requestCtx, EventsRequest{}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled caller is blocked")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("canceled worker is blocked")
	}
	stop()
	if err := queue.Events(t.Context(), EventsRequest{}); !errors.Is(err, ErrQueueStopped) {
		t.Fatalf("stopped queue accepted work: %v", err)
	}
}

func TestQueueShutdown(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "drains pending jobs"
		if timeout {
			name = "cancels jobs at deadline"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{}, 2)
			release := make(chan struct{})
			queue := NewQueue(t.Context(), 1, 1, func(ctx context.Context, _ EventsRequest) error {
				started <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			results := make(chan error, 2)
			go func() { results <- queue.Events(t.Context(), EventsRequest{}) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("worker did not start")
			}
			go func() { results <- queue.Events(t.Context(), EventsRequest{}) }()
			waitFor(t, func() bool { return queue.Health().Queue.Length == 1 })

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if timeout {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()
			}
			shutdownResult := make(chan error, 1)
			go func() { shutdownResult <- queue.Shutdown(ctx) }()
			waitFor(t, func() bool {
				queue.mu.Lock()
				defer queue.mu.Unlock()
				return queue.closed
			})
			if err := queue.Events(t.Context(), EventsRequest{}); !errors.Is(err, ErrQueueStopped) {
				t.Fatalf("shutdown admitted a job: %v", err)
			}
			if !timeout {
				select {
				case err := <-shutdownResult:
					t.Fatalf("shutdown returned before draining: %v", err)
				default:
				}
				close(release)
			}
			select {
			case err := <-shutdownResult:
				if timeout && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected shutdown deadline, got %v", err)
				}
				if !timeout && err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown blocked")
			}
			for range 2 {
				select {
				case err := <-results:
					if timeout && err == nil {
						t.Fatal("unfinished job reported success")
					}
					if !timeout && err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("caller remained blocked")
				}
			}
			select {
			case <-queue.done:
			case <-time.After(time.Second):
				t.Fatal("workers did not exit")
			}
		})
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready() {
		biff.AssertFalse(time.Now().After(deadline))

		time.Sleep(time.Millisecond)
	}
}

func TestEventsRate(t *testing.T) {
	queue := &Queue{}
	queue.events.Add(240)
	queue.sampleEvents(2 * time.Second)
	if got := queue.Health().EventsPerSecond; got != 120 {
		t.Fatalf("events/s = %d, want 120", got)
	}
	// Health reads must not reset the shared measurement.
	biff.AssertEqual(queue.Health().EventsPerSecond, uint64(120))

	queue.sampleEvents(time.Second)
	biff.AssertEqual(queue.Health().EventsPerSecond, uint64(0))

}

func TestWorkerCountsSuccessfulEvents(t *testing.T) {
	// Start only the worker so sampling cannot race the counter assertions.
	queue := &Queue{ctx: t.Context(), jobs: make(chan eventJob, 2)}
	queue.wg.Add(1)
	go queue.work(func(_ context.Context, request EventsRequest) error {
		if request.ServerID == "failed" {
			return errors.New("persistence failed")
		}
		return nil
	})
	defer func() { close(queue.jobs); queue.wg.Wait() }()
	for _, server := range []string{"ok", "failed"} {
		job := eventJob{ctx: t.Context(), request: EventsRequest{ServerID: server, Events: make([]Event, 7)}, result: make(chan error, 1)}
		queue.jobs <- job
		select {
		case <-job.result:
		case <-time.After(time.Second):
			t.Fatal("worker did not finish")
		}
	}
	queue.sampleEvents(time.Second)
	biff.AssertEqual(queue.Health().EventsPerSecond, uint64(7))
}
