// Package asyncx provides a collection of concurrency primitives and async
// utilities for building robust, non-blocking Go services.
//
// It is designed as a companion to the project's layered architecture,
// covering the most common concurrent patterns found in service and
// infrastructure layers: fan-out, worker pools, retries, timeouts,
// fire-and-forget, and rate-limiting — all with first-class context support.
//
// # Futures
//
// A [Future] represents a value that will be computed asynchronously.
// Use [Run] to start work immediately in a goroutine and [Future.Await] to
// block until the result is ready. Await is safe to call from multiple
// goroutines and caches the result after the first resolution.
//
//	fut := asyncx.Run(func() (*User, error) {
//	    return repo.GetByID(ctx, id)
//	})
//
//	// ... do other work ...
//
//	user, err := fut.Await()
//
// # Fan-out
//
// [All] runs a set of functions concurrently and collects every result in
// the original order. It returns on the first error but still waits for all
// goroutines to finish, preventing goroutine leaks.
//
//	results, err := asyncx.All(ctx,
//	    func(ctx context.Context) (*Candidate, error) { return candidateRepo.GetByID(ctx, cID) },
//	    func(ctx context.Context) (*Job, error)       { return jobRepo.GetByID(ctx, jID) },
//	)
//
// [AllSettled] behaves like [All] but never short-circuits. It always returns
// one [Result] per function so callers can inspect individual outcomes.
//
// [Race] returns the first result that arrives, regardless of success or
// failure, and cancels the remaining goroutines via context.
//
// # Concurrent Collection Helpers
//
// [Map] applies a transformation function to every element of a slice
// concurrently and returns the results in the original order.
//
//	emails, err := asyncx.Map(ctx, userIDs, func(ctx context.Context, id UserID) (string, error) {
//	    u, err := repo.GetByID(ctx, id)
//	    return string(u.Email), err
//	})
//
// [ForEach] is like [Map] but discards return values, useful for concurrent
// side-effects such as sending notifications or invalidating caches.
//
// # Worker Pool
//
// [Pool] is the bounded alternative to [Map]. It limits concurrency to a fixed
// number of workers, making it suitable for workloads that must not overwhelm
// downstream resources such as database connections or rate-limited APIs.
//
//	// Process 1 000 items with at most 10 concurrent DB calls.
//	results, err := asyncx.Pool(ctx, 10, items, func(ctx context.Context, item Item) (Result, error) {
//	    return process(ctx, item)
//	})
//
// # Retry
//
// [Retry] calls a function up to n times, returning as soon as it succeeds.
//
//	data, err := asyncx.Retry(ctx, 3, func(ctx context.Context) (*Data, error) {
//	    return client.Fetch(ctx)
//	})
//
// [RetryWithBackoff] adds exponential backoff between attempts, doubling the
// wait duration after every failure. It respects context cancellation between
// retries so the caller can abort early.
//
//	data, err := asyncx.RetryWithBackoff(ctx, 5, 100*time.Millisecond, func(ctx context.Context) (*Data, error) {
//	    return client.Fetch(ctx)
//	})
//
// # Timeout
//
// [WithTimeout] runs a function with a hard deadline. If the function does not
// finish within the given duration it returns [context.DeadlineExceeded].
//
//	result, err := asyncx.WithTimeout(ctx, 2*time.Second, func(ctx context.Context) (*Data, error) {
//	    return slowClient.Fetch(ctx)
//	})
//
// # Fire-and-Forget
//
// [Do] launches a goroutine without tracking its result, useful for
// non-critical background work such as audit logging or cache warming.
// [DoCtx] additionally checks whether the context is already cancelled
// before starting.
//
//	asyncx.Do(func() {
//	    auditLog.Record(event)
//	})
//
// # Rate-Limiting Wrappers
//
// [Debounced] wraps a function so it is only invoked after calls stop
// arriving for at least the specified duration. Every new call resets
// the timer. Useful for coalescing high-frequency events.
//
//	save := asyncx.Debounced(500*time.Millisecond, func() {
//	    index.Flush()
//	})
//
// [Throttled] wraps a function so it executes at most once per interval.
// Calls that arrive within the interval are silently dropped.
//
//	notify := asyncx.Throttled(1*time.Second, func() {
//	    metrics.Emit()
//	})
//
// # Once
//
// [Once] wraps a function so it is executed exactly once regardless of
// how many goroutines call it concurrently. The result is cached and
// returned to every subsequent caller.
//
//	loadConfig := asyncx.Once(func() (*Config, error) {
//	    return config.LoadFromEnv()
//	})
//
//	cfg, err := loadConfig() // always returns the same value
//
// # Design Notes
//
// All functions that accept a [context.Context] propagate cancellation and
// deadlines to the work they coordinate. Goroutines are never abandoned:
// every helper waits for launched goroutines to finish before returning,
// ensuring clean shutdown behaviour when contexts are cancelled.
//
// The package has no external dependencies and relies solely on the Go
// standard library.
package asyncx
