package config

import "time"

// JobxConfig configures the background job queue.
type JobxConfig struct {
	Concurrency      int
	Queues           []string
	PollInterval     time.Duration
	ShutdownTimeout  time.Duration
	DequeueTimeout   time.Duration
	DefaultRetryDelay time.Duration
}

func loadJobxConfig() JobxConfig {
	return JobxConfig{
		Concurrency:      getEnvInt("JOBX_CONCURRENCY", 4),
		Queues:           getEnvStringSlice("JOBX_QUEUES", []string{"default"}),
		PollInterval:     getEnvDuration("JOBX_POLL_INTERVAL", time.Second),
		ShutdownTimeout:  getEnvDuration("JOBX_SHUTDOWN_TIMEOUT", 30*time.Second),
		DequeueTimeout:   getEnvDuration("JOBX_DEQUEUE_TIMEOUT", 5*time.Second),
		DefaultRetryDelay: getEnvDuration("JOBX_DEFAULT_RETRY_DELAY", 30*time.Second),
	}
}
