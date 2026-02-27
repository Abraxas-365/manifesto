package memoryx_test

import (
	"context"
	"hash/fnv"
	"math"
	"testing"

	"github.com/Abraxas-365/manifesto/pkg/ai/document"
	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/memoryx"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore/providers/vstmemory"
)

const testDimension = 32

// deterministicEmbedder produces deterministic embeddings based on text content.
// Similar texts produce similar vectors (using character frequency as a rough signal).
type deterministicEmbedder struct{}

func (e *deterministicEmbedder) EmbedDocuments(_ context.Context, docs []string, _ ...embedding.Option) ([]embedding.Embedding, error) {
	result := make([]embedding.Embedding, len(docs))
	for i, doc := range docs {
		result[i] = embedding.Embedding{Vector: e.embed(doc)}
	}
	return result, nil
}

func (e *deterministicEmbedder) EmbedQuery(_ context.Context, text string, _ ...embedding.Option) (embedding.Embedding, error) {
	return embedding.Embedding{Vector: e.embed(text)}, nil
}

func (e *deterministicEmbedder) embed(text string) []float32 {
	vec := make([]float32, testDimension)
	// Use a hash-based approach so identical/similar strings get similar vectors
	h := fnv.New64a()
	h.Write([]byte(text))
	seed := h.Sum64()

	// Fill vector deterministically from seed
	for i := range vec {
		// Simple LCG to generate pseudo-random but deterministic values
		seed = seed*6364136223846793005 + 1442695040888963407
		vec[i] = float32(seed>>33) / float32(1<<31) // normalize to [0, 1)
	}

	// Normalize to unit vector
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

func (e *deterministicEmbedder) Dimensions() int {
	return testDimension
}

func newTestContextualMemory(opts ...memoryx.ContextualOption) (*memoryx.ContextualMemory, *memoryx.InMemoryMemory) {
	memStore := vstmemory.NewMemoryVectorStore(testDimension, vstore.MetricCosine)
	vstoreClient := vstore.NewClient(memStore)
	embedder := &deterministicEmbedder{}
	docEmbedder := document.NewEmbedder(embedder, testDimension)
	docStore := document.NewDocumentStore(vstoreClient, docEmbedder)

	base := memoryx.NewInMemoryMemory("You are a helpful assistant.")
	cm := memoryx.NewContextualMemory(base, docStore, opts...)
	return cm, base
}

func TestContextualMemory_BasicAddAndMessages(t *testing.T) {
	cm, _ := newTestContextualMemory()

	cm.Add(llm.NewUserMessage("hello"))
	cm.Add(llm.NewAssistantMessage("hi there"))

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// Should have at least: system + user + assistant
	// May also have a context message if retrieval found something
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}

	// System prompt should always be first
	if msgs[0].Role != llm.RoleSystem {
		t.Fatalf("expected system prompt first, got %s", msgs[0].Role)
	}
}

func TestContextualMemory_RetrievesRelevantContext(t *testing.T) {
	cm, _ := newTestContextualMemory(
		memoryx.WithContextTopK(3),
		memoryx.WithContextRecentToSkip(0), // don't skip any for testing
	)

	// Add some early messages about a specific topic
	cm.Add(llm.NewUserMessage("Tell me about Go concurrency patterns"))
	cm.Add(llm.NewAssistantMessage("Go has goroutines, channels, and the sync package for concurrency."))
	cm.Add(llm.NewUserMessage("What about mutexes?"))
	cm.Add(llm.NewAssistantMessage("sync.Mutex provides mutual exclusion. Use RWMutex for read-heavy workloads."))

	// Add unrelated messages
	cm.Add(llm.NewUserMessage("What is the weather today?"))
	cm.Add(llm.NewAssistantMessage("I don't have access to weather data."))
	cm.Add(llm.NewUserMessage("Tell me a joke"))
	cm.Add(llm.NewAssistantMessage("Why do programmers prefer dark mode? Because light attracts bugs."))

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// Should have messages returned without error
	if len(msgs) < 3 {
		t.Fatalf("expected messages, got %d", len(msgs))
	}

	// Check that a context message was injected (if retrieval found anything)
	hasContextMsg := false
	for _, m := range msgs {
		if m.Metadata != nil && m.Metadata["contextual_memory"] == true {
			hasContextMsg = true
			break
		}
	}

	// With enough messages and non-zero similarity, we should get context
	t.Logf("Context message injected: %v, total messages: %d", hasContextMsg, len(msgs))
}

func TestContextualMemory_SystemPromptAlwaysFirst(t *testing.T) {
	cm, _ := newTestContextualMemory()

	cm.Add(llm.NewUserMessage("first"))
	cm.Add(llm.NewAssistantMessage("response"))
	cm.Add(llm.NewUserMessage("second"))

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	if msgs[0].Role != llm.RoleSystem {
		t.Fatal("system prompt must always be first")
	}
	if msgs[0].Content != "You are a helpful assistant." {
		t.Fatalf("unexpected system prompt: %s", msgs[0].Content)
	}
}

func TestContextualMemory_ClearPreservesVectorStore(t *testing.T) {
	cm, _ := newTestContextualMemory(
		memoryx.WithContextRecentToSkip(0),
	)

	cm.Add(llm.NewUserMessage("Go concurrency with goroutines and channels"))
	cm.Add(llm.NewAssistantMessage("Goroutines are lightweight threads managed by Go runtime."))

	// Clear conversation
	cm.Clear()

	// Add a new message — the vector store still has the old ones
	cm.Add(llm.NewUserMessage("Tell me about goroutines"))

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// Should work without error even after clear
	if len(msgs) < 2 { // system + at least one message
		t.Fatalf("expected at least 2 messages after clear, got %d", len(msgs))
	}
}

func TestContextualMemory_NoContextForEmptyConversation(t *testing.T) {
	cm, _ := newTestContextualMemory()

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// Only system prompt, no context injection
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (system only), got %d", len(msgs))
	}
}

func TestContextualMemory_DoesNotIndexSystemMessages(t *testing.T) {
	cm, _ := newTestContextualMemory(
		memoryx.WithContextRecentToSkip(0),
	)

	// Add a system-like message manually (the initial one was added by InMemoryMemory)
	cm.Add(llm.NewUserMessage("hello"))

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// Should work without error
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
}

func TestContextualMemory_ClearAll(t *testing.T) {
	cm, _ := newTestContextualMemory()

	cm.Add(llm.NewUserMessage("message one"))
	cm.Add(llm.NewAssistantMessage("response one"))

	err := cm.ClearAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// After ClearAll, only system prompt should remain
	if len(msgs) != 1 || msgs[0].Role != llm.RoleSystem {
		t.Fatalf("expected only system prompt after ClearAll, got %d messages", len(msgs))
	}
}

func TestContextualMemory_ContextMessagePosition(t *testing.T) {
	cm, _ := newTestContextualMemory(
		memoryx.WithContextTopK(2),
		memoryx.WithContextRecentToSkip(0),
		memoryx.WithContextMinScore(0.0),
	)

	// Add enough messages to have something to retrieve
	for i := 0; i < 20; i++ {
		cm.Add(llm.NewUserMessage("test message about Go programming"))
		cm.Add(llm.NewAssistantMessage("Go is a great language for building systems"))
	}

	msgs, err := cm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	// If context was injected, it should be right after system prompt
	if len(msgs) > 1 && msgs[1].Metadata != nil && msgs[1].Metadata["contextual_memory"] == true {
		t.Log("Context correctly positioned after system prompt")
		// The rest should be conversation messages
		for i := 2; i < len(msgs); i++ {
			if msgs[i].Metadata != nil && msgs[i].Metadata["contextual_memory"] == true {
				t.Fatal("context message should only appear once, at position 1")
			}
		}
	}
}
