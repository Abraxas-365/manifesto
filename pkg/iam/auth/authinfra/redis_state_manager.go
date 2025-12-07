package authinfra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisStateManager implementación en Redis del StateManager
type RedisStateManager struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStateManager crea un nuevo state manager con Redis
func NewRedisStateManager(client *redis.Client) auth.StateManager {
	return &RedisStateManager{
		client: client,
		ttl:    10 * time.Minute, // Estados válidos por 10 minutos
	}
}

// GenerateState genera un nuevo estado OAuth
func (sm *RedisStateManager) GenerateState() string {
	return uuid.NewString()
}

// StoreState almacena un estado con sus datos asociados
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

// ValidateState valida si un estado es válido
func (sm *RedisStateManager) ValidateState(state string) bool {
	ctx := context.Background()
	key := fmt.Sprintf("oauth_state:%s", state)

	exists, err := sm.client.Exists(ctx, key).Result()
	if err != nil {
		return false
	}

	return exists == 1
}

// GetStateData obtiene los datos asociados a un estado
func (sm *RedisStateManager) GetStateData(ctx context.Context, state string) (map[string]any, error) {
	key := fmt.Sprintf("oauth_state:%s", state)

	// Obtener y eliminar el estado (one-time use)
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
