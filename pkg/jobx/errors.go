package jobx

import "github.com/Abraxas-365/manifesto/pkg/errx"

var jobxErrors = errx.NewRegistry("JOBX")

var (
	ErrJobNotFound      = jobxErrors.Register("JOB_NOT_FOUND", errx.TypeNotFound, 404, "Job not found")
	ErrEnqueueFailed    = jobxErrors.Register("ENQUEUE_FAILED", errx.TypeExternal, 500, "Failed to enqueue job")
	ErrDequeueFailed    = jobxErrors.Register("DEQUEUE_FAILED", errx.TypeExternal, 500, "Failed to dequeue job")
	ErrCompleteFailed   = jobxErrors.Register("COMPLETE_FAILED", errx.TypeExternal, 500, "Failed to complete job")
	ErrFailFailed       = jobxErrors.Register("FAIL_FAILED", errx.TypeExternal, 500, "Failed to mark job as failed")
	ErrRetryFailed      = jobxErrors.Register("RETRY_FAILED", errx.TypeExternal, 500, "Failed to retry job")
	ErrNoHandler        = jobxErrors.Register("NO_HANDLER", errx.TypeValidation, 400, "No handler registered for job type")
	ErrInvalidJob       = jobxErrors.Register("INVALID_JOB", errx.TypeValidation, 400, "Invalid job definition")
	ErrAlreadyRunning   = jobxErrors.Register("ALREADY_RUNNING", errx.TypeConflict, 409, "Worker is already running")
	ErrShutdownTimeout  = jobxErrors.Register("SHUTDOWN_TIMEOUT", errx.TypeInternal, 500, "Graceful shutdown timed out")
)
