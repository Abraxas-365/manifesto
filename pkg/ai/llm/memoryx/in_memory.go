package memoryx

import (
	"sync"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
)

// InMemoryMemory is a simple in-memory implementation of the Memory interface.
// It stores messages in a slice and preserves the system prompt on Clear().
type InMemoryMemory struct {
	mu       sync.RWMutex
	messages []llm.Message
}

// NewInMemoryMemory creates a new in-memory memory, optionally with a system prompt.
func NewInMemoryMemory(systemPrompt ...string) *InMemoryMemory {
	m := &InMemoryMemory{}
	if len(systemPrompt) > 0 && systemPrompt[0] != "" {
		m.messages = []llm.Message{llm.NewSystemMessage(systemPrompt[0])}
	}
	return m
}

func (m *InMemoryMemory) Messages() ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]llm.Message, len(m.messages))
	copy(out, m.messages)
	return out, nil
}

func (m *InMemoryMemory) Add(message llm.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	return nil
}

func (m *InMemoryMemory) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Preserve system prompt if present
	var systemPrompt *llm.Message
	if len(m.messages) > 0 && m.messages[0].Role == llm.RoleSystem {
		sp := m.messages[0]
		systemPrompt = &sp
	}

	m.messages = nil
	if systemPrompt != nil {
		m.messages = []llm.Message{*systemPrompt}
	}
	return nil
}
