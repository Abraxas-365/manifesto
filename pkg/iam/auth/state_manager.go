package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// InMemoryStateManager implementación en memoria del StateManager
type InMemoryStateManager struct {
	states map[string]*stateEntry
	mu     sync.RWMutex
}

type stateEntry struct {
	data      map[string]any
	expiresAt time.Time
}

// NewInMemoryStateManager crea un nuevo state manager en memoria
func NewInMemoryStateManager() *InMemoryStateManager {
	sm := &InMemoryStateManager{
		states: make(map[string]*stateEntry),
	}

	// Cleanup goroutine para estados expirados
	go sm.cleanup()

	return sm
}

// GenerateState genera un nuevo estado OAuth
func (sm *InMemoryStateManager) GenerateState() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback en caso de error
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// StoreState almacena un estado con sus datos asociados
func (sm *InMemoryStateManager) StoreState(ctx context.Context, state string, data map[string]any) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.states[state] = &stateEntry{
		data:      data,
		expiresAt: time.Now().Add(10 * time.Minute), // Estados válidos por 10 minutos
	}

	return nil
}

// ValidateState valida si un estado es válido
func (sm *InMemoryStateManager) ValidateState(state string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	entry, exists := sm.states[state]
	if !exists {
		return false
	}

	return time.Now().Before(entry.expiresAt)
}

// GetStateData obtiene los datos asociados a un estado
func (sm *InMemoryStateManager) GetStateData(ctx context.Context, state string) (map[string]any, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry, exists := sm.states[state]
	if !exists {
		return nil, ErrInvalidState()
	}

	if time.Now().After(entry.expiresAt) {
		delete(sm.states, state)
		return nil, ErrInvalidState()
	}

	// Eliminar el estado después de usarlo (one-time use)
	data := entry.data
	delete(sm.states, state)

	return data, nil
}

// cleanup limpia estados expirados periodicamente
func (sm *InMemoryStateManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		for state, entry := range sm.states {
			if now.After(entry.expiresAt) {
				delete(sm.states, state)
			}
		}
		sm.mu.Unlock()
	}
}
