package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// InMemoryStateManager is an in-memory implementation of StateManager
type InMemoryStateManager struct {
	states map[string]*stateEntry
	mu     sync.RWMutex
	ttl    time.Duration // Add this field
}

type stateEntry struct {
	data      map[string]any
	expiresAt time.Time
}

// NewInMemoryStateManager creates a new in-memory state manager
func NewInMemoryStateManager(ttl time.Duration) *InMemoryStateManager {
	sm := &InMemoryStateManager{
		states: make(map[string]*stateEntry),
		ttl:    ttl,
	}
	go sm.cleanup()
	return sm
}

// GenerateState generates a new OAuth state
func (sm *InMemoryStateManager) GenerateState() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback on error
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// StoreState stores a state with its associated data
func (sm *InMemoryStateManager) StoreState(ctx context.Context, state string, data map[string]any) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.states[state] = &stateEntry{
		data:      data,
		expiresAt: time.Now().Add(sm.ttl),
	}

	return nil
}

// ValidateState checks whether a state is valid
func (sm *InMemoryStateManager) ValidateState(state string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	entry, exists := sm.states[state]
	if !exists {
		return false
	}

	return time.Now().Before(entry.expiresAt)
}

// GetStateData retrieves the data associated with a state
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

	// Delete the state after use (one-time use)
	data := entry.data
	delete(sm.states, state)

	return data, nil
}

// cleanup periodically removes expired states
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
