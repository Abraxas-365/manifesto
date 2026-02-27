package jobx

import (
	"context"
	"sync"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/logx"
)

// HandlerFunc processes a job. Return nil on success, an error to trigger retry/fail.
type HandlerFunc func(ctx context.Context, job *JobInfo) error

// JobEnqueuer enqueues jobs for processing.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, job Job) (string, error)
	EnqueueDelayed(ctx context.Context, job Job, delay time.Duration) (string, error)
}

// JobStatusReader reads job status.
type JobStatusReader interface {
	GetJob(ctx context.Context, jobID string) (*JobInfo, error)
}

// JobProcessor provides backend operations for the worker loop.
type JobProcessor interface {
	Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*JobInfo, error)
	Complete(ctx context.Context, jobID string, result []byte) error
	Fail(ctx context.Context, jobID string, errMsg string) (retry bool, err error)
	Retry(ctx context.Context, jobID string, delay time.Duration) error
	PromoteScheduled(ctx context.Context, queues []string) error
}

// Queue combines all backend operations.
type Queue interface {
	JobEnqueuer
	JobStatusReader
	JobProcessor
}

// Client is the main entry point for enqueuing and processing jobs.
type Client struct {
	queue    Queue
	opts     WorkerOptions
	handlers map[string]HandlerFunc
	mu       sync.RWMutex
	running  bool
}

// NewClient creates a new job processing client.
func NewClient(queue Queue, options ...WorkerOption) *Client {
	opts := defaultWorkerOptions()
	for _, o := range options {
		o(&opts)
	}
	return &Client{
		queue:    queue,
		opts:     opts,
		handlers: make(map[string]HandlerFunc),
	}
}

// Register adds a handler for a given job type.
func (c *Client) Register(jobType string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[jobType] = handler
}

// Enqueue enqueues a job for immediate processing.
func (c *Client) Enqueue(ctx context.Context, job Job) (string, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = 3
	}
	return c.queue.Enqueue(ctx, job)
}

// EnqueueDelayed enqueues a job with a delay before it becomes available.
func (c *Client) EnqueueDelayed(ctx context.Context, job Job, delay time.Duration) (string, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = 3
	}
	return c.queue.EnqueueDelayed(ctx, job, delay)
}

// GetJob returns the current state of a job.
func (c *Client) GetJob(ctx context.Context, jobID string) (*JobInfo, error) {
	return c.queue.GetJob(ctx, jobID)
}

// Start begins processing jobs. It blocks until ctx is cancelled.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return jobxErrors.New(ErrAlreadyRunning)
	}
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	logx.Infof("jobx: starting %d workers on queues %v", c.opts.Concurrency, c.opts.Queues)

	var wg sync.WaitGroup

	// Scheduler goroutine: promotes delayed jobs to the ready queue.
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.schedulerLoop(ctx)
	}()

	// Worker goroutines.
	for i := range c.opts.Concurrency {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c.workerLoop(ctx, id)
		}(i)
	}

	// Wait for context cancellation, then drain.
	<-ctx.Done()
	logx.Info("jobx: shutting down workers...")

	// Give workers time to finish current jobs.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logx.Info("jobx: all workers stopped")
	case <-time.After(c.opts.ShutdownTimeout):
		logx.Warn("jobx: shutdown timed out, some jobs may not have completed")
	}

	return nil
}

func (c *Client) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(c.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.queue.PromoteScheduled(ctx, c.opts.Queues); err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.WithError(err).Warn("jobx: failed to promote scheduled jobs")
			}
		}
	}
}

func (c *Client) workerLoop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, err := c.queue.Dequeue(ctx, c.opts.Queues, c.opts.DequeueTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logx.WithError(err).Warnf("jobx: worker %d dequeue error", id)
			time.Sleep(c.opts.PollInterval)
			continue
		}
		if job == nil {
			continue
		}

		c.processJob(ctx, job)
	}
}

func (c *Client) processJob(ctx context.Context, job *JobInfo) {
	c.mu.RLock()
	handler, ok := c.handlers[job.Type]
	c.mu.RUnlock()

	if !ok {
		logx.Warnf("jobx: no handler for job type %q (id=%s)", job.Type, job.ID)
		_, _ = c.queue.Fail(ctx, job.ID, "no handler registered for job type")
		return
	}

	if err := handler(ctx, job); err != nil {
		logx.WithError(err).Warnf("jobx: job %s (type=%s) failed", job.ID, job.Type)

		shouldRetry, failErr := c.queue.Fail(ctx, job.ID, err.Error())
		if failErr != nil {
			logx.WithError(failErr).Errorf("jobx: failed to mark job %s as failed", job.ID)
			return
		}

		if shouldRetry {
			if retryErr := c.queue.Retry(ctx, job.ID, c.opts.DefaultRetryDelay); retryErr != nil {
				logx.WithError(retryErr).Errorf("jobx: failed to retry job %s", job.ID)
			}
		}
		return
	}

	if err := c.queue.Complete(ctx, job.ID, nil); err != nil {
		logx.WithError(err).Errorf("jobx: failed to complete job %s", job.ID)
	}
}
