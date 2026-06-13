package pipeline

// pipeline_test.go is the fixed specification for the concurrent fan-out/fan-in
// pipeline. The agent implements Pipeline; it may not edit this file.
//
// The done-gate runs `go test` but never `go test -race` (the harness pins
// CGO_ENABLED=0), so this spec proves the things a race-free gate *can* prove
// deterministically — correct, in-order output; real >=workers fan-out; clean
// termination; prompt cancellation — by injecting an fn that blocks until the
// workers rendezvous, and by bounding every call with an in-test timeout so a
// missing fan-out or a forgotten cancellation surfaces as a clear failure
// instead of a hang. Race-freedom, goroutine-leak freedom, and the exact-worker
// upper bound are deliberately out of scope here: they are read off the code by
// the out-of-loop judge, not gated.

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// run executes Pipeline in a goroutine and fails the test if it does not return
// within d. The result channel is buffered so the worker goroutine can always
// send (and thus exit) even on the timeout path — the helper itself never leaks.
func run(t *testing.T, d time.Duration, f func() ([]int, error)) ([]int, error) {
	t.Helper()
	type result struct {
		got []int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		got, err := f()
		ch <- result{got, err}
	}()
	select {
	case r := <-ch:
		return r.got, r.err
	case <-time.After(d):
		t.Fatalf("Pipeline did not return within %s", d)
		return nil, nil
	}
}

// TestOrderPreservedAndCorrect checks the core contract: fn applied to every
// element, results in input order, for several worker counts. Order is the
// deterministic discriminator — a fan-in that appends in completion order would
// fail this even though it processes every item.
func TestOrderPreservedAndCorrect(t *testing.T) {
	in := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	want := make([]int, len(in))
	for i, n := range in {
		want[i] = n * n
	}
	square := func(_ context.Context, n int) int { return n * n }

	for _, workers := range []int{1, 3, 7} {
		got, err := run(t, 2*time.Second, func() ([]int, error) {
			return Pipeline(context.Background(), in, workers, square)
		})
		if err != nil {
			t.Errorf("workers=%d: unexpected error: %v", workers, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("workers=%d: got %v, want %v", workers, got, want)
		}
	}
}

// TestFanOutIsConcurrent proves the work actually fans out to at least `workers`
// goroutines at once. Each fn call parks until `workers` of them are
// simultaneously in flight (the barrier), so a sequential implementation can
// never get past the first item: the lone worker waits on a barrier that needs
// `workers` arrivals, run times out, and the test fails. A correct pool reaches
// the barrier, releases, and returns the results in order.
func TestFanOutIsConcurrent(t *testing.T) {
	const workers = 4
	in := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

	var mu sync.Mutex
	active, peak := 0, 0
	reached := make(chan struct{})
	var once sync.Once

	fn := func(ctx context.Context, n int) int {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		if active == workers {
			once.Do(func() { close(reached) })
		}
		mu.Unlock()

		// Hold the worker until the pool is full, so concurrency is observable.
		select {
		case <-reached:
		case <-ctx.Done():
		}

		mu.Lock()
		active--
		mu.Unlock()
		return n
	}

	got, err := run(t, 2*time.Second, func() ([]int, error) {
		return Pipeline(context.Background(), in, workers, fn)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak < workers {
		t.Fatalf("fan-out not concurrent: peak in-flight = %d, want >= %d", gotPeak, workers)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}

// TestCancelMidFlightReturnsErr cancels the context while a worker is parked
// inside fn. Pipeline must stop promptly, report ctx.Err(), and discard any
// partial results rather than returning a short slice.
func TestCancelMidFlightReturnsErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	fn := func(ctx context.Context, n int) int {
		select {
		case started <- struct{}{}: // signal the first entry (non-blocking)
		default:
		}
		<-ctx.Done() // park until cancelled
		return n
	}

	go func() {
		<-started
		cancel()
	}()

	got, err := run(t, 2*time.Second, func() ([]int, error) {
		return Pipeline(ctx, []int{0, 1, 2, 3, 4, 5}, 3, fn)
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no results after cancellation", got)
	}
}

// TestAlreadyCancelled passes a context that is already cancelled. Pipeline must
// return ctx.Err() and no results without hanging.
func TestAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int32
	fn := func(_ context.Context, n int) int {
		atomic.AddInt32(&calls, 1)
		return n
	}

	got, err := run(t, 2*time.Second, func() ([]int, error) {
		return Pipeline(ctx, []int{1, 2, 3}, 2, fn)
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no results", got)
	}
}

// TestEmptyInput: no work to do is success, not an error, and must not hang.
func TestEmptyInput(t *testing.T) {
	fn := func(_ context.Context, n int) int { return n }
	for _, in := range [][]int{nil, {}} {
		got, err := run(t, 2*time.Second, func() ([]int, error) {
			return Pipeline(context.Background(), in, 4, fn)
		})
		if err != nil {
			t.Errorf("in=%v: unexpected error: %v", in, err)
		}
		if len(got) != 0 {
			t.Errorf("in=%v: got %v, want empty", in, got)
		}
	}
}

// TestInvalidWorkers: a non-positive worker count is a usage error.
func TestInvalidWorkers(t *testing.T) {
	fn := func(_ context.Context, n int) int { return n }
	for _, workers := range []int{0, -1} {
		got, err := run(t, 2*time.Second, func() ([]int, error) {
			return Pipeline(context.Background(), []int{1, 2}, workers, fn)
		})
		if err == nil {
			t.Errorf("workers=%d: expected error, got nil", workers)
		}
		if len(got) != 0 {
			t.Errorf("workers=%d: got %v, want no results", workers, got)
		}
	}
}
