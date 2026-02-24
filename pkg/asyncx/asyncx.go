package asyncx

import (
	"context"
	"sync"
	"time"
)

// ─── Future ──────────────────────────────────────────────────────────────────

// result holds the outcome of an async computation.
type result[T any] struct {
	value T
	err   error
}

// Future represents a value that will be available asynchronously.
// Create one with Run and retrieve its value with Await.
type Future[T any] struct {
	ch  chan result[T]
	res *result[T]
	mu  sync.Mutex
}

// Run executes fn in a goroutine and returns a Future for its result.
// The goroutine starts immediately.
func Run[T any](fn func() (T, error)) *Future[T] {
	f := &Future[T]{ch: make(chan result[T], 1)}
	go func() {
		v, err := fn()
		f.ch <- result[T]{value: v, err: err}
	}()
	return f
}

// Await blocks until the Future completes and returns its value and error.
// Safe to call multiple times — subsequent calls return the cached result.
func (f *Future[T]) Await() (T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.res == nil {
		r := <-f.ch
		f.res = &r
	}
	return f.res.value, f.res.err
}

// ─── Concurrency Primitives ───────────────────────────────────────────────────

// Do fires fn in a goroutine and forgets it (fire-and-forget).
func Do(fn func()) {
	go fn()
}

// DoCtx fires fn in a goroutine only if ctx is not already done.
func DoCtx(ctx context.Context, fn func(context.Context)) {
	go func() {
		select {
		case <-ctx.Done():
			return
		default:
			fn(ctx)
		}
	}()
}

// ─── All / Race ───────────────────────────────────────────────────────────────

// All runs all fns concurrently and waits for every one to finish.
// Returns a slice of results in the same order as the input functions.
// If any function returns an error the first error is returned; other
// goroutines are still awaited so resources are not leaked.
func All[T any](ctx context.Context, fns ...func(context.Context) (T, error)) ([]T, error) {
	results := make([]T, len(fns))
	errs := make([]error, len(fns))

	var wg sync.WaitGroup
	wg.Add(len(fns))

	for i, fn := range fns {
		i, fn := i, fn
		go func() {
			defer wg.Done()
			results[i], errs[i] = fn(ctx)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// AllSettled runs all fns concurrently and waits for every one to finish.
// Unlike All it never short-circuits: it always returns one Result per fn.
func AllSettled[T any](ctx context.Context, fns ...func(context.Context) (T, error)) []Result[T] {
	results := make([]Result[T], len(fns))
	var wg sync.WaitGroup
	wg.Add(len(fns))

	for i, fn := range fns {
		i, fn := i, fn
		go func() {
			defer wg.Done()
			v, err := fn(ctx)
			results[i] = Result[T]{Value: v, Err: err}
		}()
	}
	wg.Wait()
	return results
}

// Result holds the outcome of a single settled async operation.
type Result[T any] struct {
	Value T
	Err   error
}

// OK reports whether the result carries no error.
func (r Result[T]) OK() bool { return r.Err == nil }

// Race runs all fns concurrently and returns the first result that arrives
// (whether success or error). Remaining goroutines are still awaited.
func Race[T any](ctx context.Context, fns ...func(context.Context) (T, error)) (T, error) {
	ch := make(chan result[T], len(fns))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, fn := range fns {
		fn := fn
		go func() {
			v, err := fn(ctx)
			ch <- result[T]{value: v, err: err}
		}()
	}

	r := <-ch
	return r.value, r.err
}

// ─── Map / Filter ─────────────────────────────────────────────────────────────

// Map applies fn to every item in items concurrently and returns the
// transformed slice in the original order. Stops and returns on the first error.
func Map[T any, R any](ctx context.Context, items []T, fn func(context.Context, T) (R, error)) ([]R, error) {
	results := make([]R, len(items))
	errs := make([]error, len(items))

	var wg sync.WaitGroup
	wg.Add(len(items))

	for i, item := range items {
		i, item := i, item
		go func() {
			defer wg.Done()
			results[i], errs[i] = fn(ctx, item)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// ForEach applies fn to every item in items concurrently.
// Returns the first error encountered, after all goroutines have finished.
func ForEach[T any](ctx context.Context, items []T, fn func(context.Context, T) error) error {
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	wg.Add(len(items))

	for i, item := range items {
		i, item := i, item
		go func() {
			defer wg.Done()
			errs[i] = fn(ctx, item)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ─── Worker Pool ──────────────────────────────────────────────────────────────

// Pool processes items using at most workers goroutines and returns results
// in the original order. Returns the first error encountered.
//
// Use this instead of Map when the number of items is large and unbounded
// concurrency would be harmful (e.g. DB connections, rate-limited APIs).
func Pool[T any, R any](
	ctx context.Context,
	workers int,
	items []T,
	fn func(context.Context, T) (R, error),
) ([]R, error) {
	if workers <= 0 {
		workers = 1
	}

	type indexed struct {
		i    int
		item T
	}

	work := make(chan indexed, len(items))
	for i, item := range items {
		work <- indexed{i: i, item: item}
	}
	close(work)

	results := make([]R, len(items))
	errs := make([]error, len(items))

	var wg sync.WaitGroup
	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()
			for w := range work {
				select {
				case <-ctx.Done():
					errs[w.i] = ctx.Err()
					return
				default:
					results[w.i], errs[w.i] = fn(ctx, w.item)
				}
			}
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// ─── Retry ────────────────────────────────────────────────────────────────────

// Retry calls fn up to attempts times, returning as soon as fn succeeds.
// Returns the last error if all attempts fail.
func Retry[T any](ctx context.Context, attempts int, fn func(context.Context) (T, error)) (T, error) {
	var (
		zero T
		err  error
		val  T
	)
	for i := range attempts {
		_ = i
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}
		val, err = fn(ctx)
		if err == nil {
			return val, nil
		}
	}
	return zero, err
}

// RetryWithBackoff calls fn up to attempts times with exponential backoff
// starting at initialDelay. The delay doubles after each failed attempt.
// Respects context cancellation between retries.
func RetryWithBackoff[T any](
	ctx context.Context,
	attempts int,
	initialDelay time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	var (
		zero  T
		err   error
		val   T
		delay = initialDelay
	)
	for i := range attempts {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		val, err = fn(ctx)
		if err == nil {
			return val, nil
		}

		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
				delay *= 2
			}
		}
	}
	return zero, err
}

// ─── Timeout ──────────────────────────────────────────────────────────────────

// WithTimeout runs fn with a deadline of d.
// Returns context.DeadlineExceeded if fn does not finish in time.
func WithTimeout[T any](ctx context.Context, d time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	type res struct {
		v   T
		err error
	}

	ch := make(chan res, 1)
	go func() {
		v, err := fn(ctx)
		ch <- res{v, err}
	}()

	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// ─── Debounce / Throttle ──────────────────────────────────────────────────────

// Debounced wraps fn so that it is only called after it stops being invoked
// for at least wait. Every call resets the timer. Thread-safe.
func Debounced(wait time.Duration, fn func()) func() {
	var (
		mu    sync.Mutex
		timer *time.Timer
	)
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(wait, fn)
	}
}

// Throttled wraps fn so that it is called at most once per interval.
// Calls that arrive while the interval has not elapsed are dropped.
// Thread-safe.
func Throttled(interval time.Duration, fn func()) func() {
	var (
		mu   sync.Mutex
		last time.Time
	)
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(last) >= interval {
			last = time.Now()
			go fn()
		}
	}
}

// ─── Once ─────────────────────────────────────────────────────────────────────

// Once wraps fn so it executes at most once, regardless of how many goroutines
// call the returned function simultaneously.
func Once[T any](fn func() (T, error)) func() (T, error) {
	var (
		once sync.Once
		val  T
		err  error
	)
	return func() (T, error) {
		once.Do(func() {
			val, err = fn()
		})
		return val, err
	}
}
