package jobxredis

import "github.com/Abraxas-365/manifesto/pkg/errx"

var redisErrors = errx.NewRegistry("JOBX_REDIS")

var (
	ErrEnqueue   = redisErrors.Register("ENQUEUE", errx.TypeExternal, 500, "Redis enqueue failed")
	ErrDequeue   = redisErrors.Register("DEQUEUE", errx.TypeExternal, 500, "Redis dequeue failed")
	ErrGetJob    = redisErrors.Register("GET_JOB", errx.TypeExternal, 500, "Redis get job failed")
	ErrComplete  = redisErrors.Register("COMPLETE", errx.TypeExternal, 500, "Redis complete failed")
	ErrFail      = redisErrors.Register("FAIL", errx.TypeExternal, 500, "Redis fail failed")
	ErrRetry     = redisErrors.Register("RETRY", errx.TypeExternal, 500, "Redis retry failed")
	ErrPromote   = redisErrors.Register("PROMOTE", errx.TypeExternal, 500, "Redis promote failed")
	ErrNotFound  = redisErrors.Register("NOT_FOUND", errx.TypeNotFound, 404, "Job not found in Redis")
	ErrMarshal   = redisErrors.Register("MARSHAL", errx.TypeInternal, 500, "Failed to marshal job data")
	ErrUnmarshal = redisErrors.Register("UNMARSHAL", errx.TypeInternal, 500, "Failed to unmarshal job data")
)
