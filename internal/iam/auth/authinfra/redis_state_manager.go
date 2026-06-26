package authinfra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisStateManager is the Redis implementation of StateManager
type RedisStateManager struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStateManager creates a new Redis-backed state manager
func NewRedisStateManager(client *redis.Client, ttl time.Duration) auth.StateManager {
	return &RedisStateManager{
		client: client,
		ttl:    ttl,
	}
}

// GenerateState generates a new OAuth state
func (sm *RedisStateManager) GenerateState() string {
	return uuid.NewString()
}

// StoreState stores a state with its associated data
func (sm *RedisStateManager) StoreState(ctx context.Context, state string, data map[string]any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal state data: %w", err)
	}

	key := fmt.Sprintf("oauth_state:%s", state)
	err = sm.client.Set(ctx, key, jsonData, sm.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store state in Redis: %w", err)
	}

	return nil
}

// ValidateState checks if a state is valid
func (sm *RedisStateManager) ValidateState(state string) bool {
	ctx := context.Background()
	key := fmt.Sprintf("oauth_state:%s", state)

	exists, err := sm.client.Exists(ctx, key).Result()
	if err != nil {
		return false
	}

	return exists == 1
}

// GetStateData retrieves the data associated with a state
func (sm *RedisStateManager) GetStateData(ctx context.Context, state string) (map[string]any, error) {
	key := fmt.Sprintf("oauth_state:%s", state)

	// Get and delete the state (one-time use)
	jsonData, err := sm.client.GetDel(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, auth.ErrInvalidState()
		}
		return nil, fmt.Errorf("failed to get state from Redis: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state data: %w", err)
	}

	return data, nil
}
