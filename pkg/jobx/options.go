package jobx

import "time"

// WorkerOptions configures the job processing client.
type WorkerOptions struct {
	Queues           []string
	Concurrency      int
	PollInterval     time.Duration
	ShutdownTimeout  time.Duration
	DequeueTimeout   time.Duration
	DefaultRetryDelay time.Duration
}

func defaultWorkerOptions() WorkerOptions {
	return WorkerOptions{
		Queues:           []string{"default"},
		Concurrency:      4,
		PollInterval:     time.Second,
		ShutdownTimeout:  30 * time.Second,
		DequeueTimeout:   5 * time.Second,
		DefaultRetryDelay: 30 * time.Second,
	}
}

// WorkerOption is a functional option for configuring the client.
type WorkerOption func(*WorkerOptions)

// WithQueues sets the queues to process.
func WithQueues(queues ...string) WorkerOption {
	return func(o *WorkerOptions) {
		o.Queues = queues
	}
}

// WithConcurrency sets the number of worker goroutines.
func WithConcurrency(n int) WorkerOption {
	return func(o *WorkerOptions) {
		if n > 0 {
			o.Concurrency = n
		}
	}
}

// WithPollInterval sets the interval between dequeue attempts when idle.
func WithPollInterval(d time.Duration) WorkerOption {
	return func(o *WorkerOptions) {
		o.PollInterval = d
	}
}

// WithShutdownTimeout sets the maximum time to wait for workers to finish on shutdown.
func WithShutdownTimeout(d time.Duration) WorkerOption {
	return func(o *WorkerOptions) {
		o.ShutdownTimeout = d
	}
}

// WithDequeueTimeout sets the timeout passed to the blocking dequeue call.
func WithDequeueTimeout(d time.Duration) WorkerOption {
	return func(o *WorkerOptions) {
		o.DequeueTimeout = d
	}
}

// WithDefaultRetryDelay sets the default delay before retrying a failed job.
func WithDefaultRetryDelay(d time.Duration) WorkerOption {
	return func(o *WorkerOptions) {
		o.DefaultRetryDelay = d
	}
}
