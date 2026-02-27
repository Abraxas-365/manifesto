package jobxredis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/jobx"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisQueue implements jobx.Queue backed by Redis.
type RedisQueue struct {
	rdb *redis.Client
}

// NewRedisQueue creates a new Redis-backed queue.
func NewRedisQueue(rdb *redis.Client) *RedisQueue {
	return &RedisQueue{rdb: rdb}
}

// Key helpers
func queueKey(name string) string    { return fmt.Sprintf("jobx:queue:%s", name) }
func scheduledKey(name string) string { return fmt.Sprintf("jobx:scheduled:%s", name) }
func jobKey(id string) string         { return fmt.Sprintf("jobx:job:%s", id) }

// Enqueue adds a job to the ready queue immediately.
func (q *RedisQueue) Enqueue(ctx context.Context, job jobx.Job) (string, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	info := jobx.JobInfo{
		ID:         id,
		Type:       job.Type,
		Queue:      job.Queue,
		Payload:    job.Payload,
		Status:     jobx.JobStatusPending,
		MaxRetries: job.MaxRetries,
		Attempts:   0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return "", redisErrors.NewWithCause(ErrMarshal, err)
	}

	pipe := q.rdb.Pipeline()
	pipe.Set(ctx, jobKey(id), data, 0)
	pipe.LPush(ctx, queueKey(job.Queue), id)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", redisErrors.NewWithCause(ErrEnqueue, err).WithDetail("queue", job.Queue)
	}

	return id, nil
}

// EnqueueDelayed adds a job to the scheduled set with a future execution time.
func (q *RedisQueue) EnqueueDelayed(ctx context.Context, job jobx.Job, delay time.Duration) (string, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	info := jobx.JobInfo{
		ID:         id,
		Type:       job.Type,
		Queue:      job.Queue,
		Payload:    job.Payload,
		Status:     jobx.JobStatusPending,
		MaxRetries: job.MaxRetries,
		Attempts:   0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return "", redisErrors.NewWithCause(ErrMarshal, err)
	}

	score := float64(now.Add(delay).Unix())

	pipe := q.rdb.Pipeline()
	pipe.Set(ctx, jobKey(id), data, 0)
	pipe.ZAdd(ctx, scheduledKey(job.Queue), redis.Z{Score: score, Member: id})
	if _, err := pipe.Exec(ctx); err != nil {
		return "", redisErrors.NewWithCause(ErrEnqueue, err).
			WithDetail("queue", job.Queue).
			WithDetail("delay", delay.String())
	}

	return id, nil
}

// GetJob retrieves job info by ID.
func (q *RedisQueue) GetJob(ctx context.Context, jobID string) (*jobx.JobInfo, error) {
	data, err := q.rdb.Get(ctx, jobKey(jobID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, redisErrors.New(ErrNotFound).WithDetail("job_id", jobID)
		}
		return nil, redisErrors.NewWithCause(ErrGetJob, err).WithDetail("job_id", jobID)
	}

	var info jobx.JobInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, redisErrors.NewWithCause(ErrUnmarshal, err).WithDetail("job_id", jobID)
	}

	return &info, nil
}

// Dequeue blocks until a job is available from one of the given queues or the timeout expires.
func (q *RedisQueue) Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*jobx.JobInfo, error) {
	keys := make([]string, len(queues))
	for i, name := range queues {
		keys[i] = queueKey(name)
	}

	result, err := q.rdb.BRPop(ctx, timeout, keys...).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no job
		}
		if ctx.Err() != nil {
			return nil, nil // context cancelled
		}
		return nil, redisErrors.NewWithCause(ErrDequeue, err)
	}

	// result[0] = key, result[1] = job ID
	jobID := result[1]

	info, err := q.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Mark as active
	info.Status = jobx.JobStatusActive
	info.Attempts++
	info.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(info)
	if err != nil {
		return nil, redisErrors.NewWithCause(ErrMarshal, err).WithDetail("job_id", jobID)
	}

	if err := q.rdb.Set(ctx, jobKey(jobID), data, 0).Err(); err != nil {
		return nil, redisErrors.NewWithCause(ErrDequeue, err).WithDetail("job_id", jobID)
	}

	return info, nil
}

// Complete marks a job as successfully completed.
func (q *RedisQueue) Complete(ctx context.Context, jobID string, result []byte) error {
	info, err := q.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	info.Status = jobx.JobStatusCompleted
	info.Result = result
	info.UpdatedAt = time.Now().UTC()

	data, mErr := json.Marshal(info)
	if mErr != nil {
		return redisErrors.NewWithCause(ErrMarshal, mErr).WithDetail("job_id", jobID)
	}

	if err := q.rdb.Set(ctx, jobKey(jobID), data, 0).Err(); err != nil {
		return redisErrors.NewWithCause(ErrComplete, err).WithDetail("job_id", jobID)
	}

	return nil
}

// Fail marks a job as failed. Returns true if the job should be retried.
func (q *RedisQueue) Fail(ctx context.Context, jobID string, errMsg string) (bool, error) {
	info, err := q.GetJob(ctx, jobID)
	if err != nil {
		return false, err
	}

	shouldRetry := info.Attempts < info.MaxRetries

	if shouldRetry {
		info.Status = jobx.JobStatusRetrying
	} else {
		info.Status = jobx.JobStatusFailed
	}
	info.Error = errMsg
	info.UpdatedAt = time.Now().UTC()

	data, mErr := json.Marshal(info)
	if mErr != nil {
		return false, redisErrors.NewWithCause(ErrMarshal, mErr).WithDetail("job_id", jobID)
	}

	if err := q.rdb.Set(ctx, jobKey(jobID), data, 0).Err(); err != nil {
		return false, redisErrors.NewWithCause(ErrFail, err).WithDetail("job_id", jobID)
	}

	return shouldRetry, nil
}

// Retry re-enqueues a failed job with a delay.
func (q *RedisQueue) Retry(ctx context.Context, jobID string, delay time.Duration) error {
	info, err := q.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	score := float64(time.Now().UTC().Add(delay).Unix())

	if err := q.rdb.ZAdd(ctx, scheduledKey(info.Queue), redis.Z{
		Score:  score,
		Member: jobID,
	}).Err(); err != nil {
		return redisErrors.NewWithCause(ErrRetry, err).WithDetail("job_id", jobID)
	}

	return nil
}

// PromoteScheduled moves jobs whose scheduled time has passed from the sorted set to the ready queue.
// Uses a Lua script for atomicity.
var promoteScript = redis.NewScript(`
local scheduled_key = KEYS[1]
local queue_key = KEYS[2]
local now = tonumber(ARGV[1])
local ids = redis.call('ZRANGEBYSCORE', scheduled_key, '-inf', now)
if #ids > 0 then
    for _, id in ipairs(ids) do
        redis.call('LPUSH', queue_key, id)
    end
    redis.call('ZREMRANGEBYSCORE', scheduled_key, '-inf', now)
end
return #ids
`)

func (q *RedisQueue) PromoteScheduled(ctx context.Context, queues []string) error {
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)

	for _, name := range queues {
		err := promoteScript.Run(ctx, q.rdb,
			[]string{scheduledKey(name), queueKey(name)},
			now,
		).Err()

		if err != nil && err != redis.Nil {
			return redisErrors.NewWithCause(ErrPromote, err).WithDetail("queue", name)
		}
	}

	return nil
}
