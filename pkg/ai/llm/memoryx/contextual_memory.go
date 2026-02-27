package memoryx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"

	"github.com/Abraxas-365/manifesto/pkg/ai/document"
)

// ContextualMemory wraps any Memory and augments it with semantic retrieval
// from a vector store. Every message added is embedded and stored. When
// Messages() is called, it retrieves the most relevant past messages based
// on the current conversation and injects them as context.
//
// This is provider-agnostic — it works with any embedding model and any
// vector store backend (pgvector, in-memory, Pinecone, etc.) through
// the document.DocumentStore abstraction.
type ContextualMemory struct {
	mu sync.Mutex

	inner    Memory
	docStore *document.DocumentStore

	// TopK is how many relevant past messages to retrieve. Defaults to 5.
	TopK int

	// MinScore is the minimum similarity score for retrieved messages.
	// Messages below this threshold are excluded. Defaults to 0.0 (no filter).
	MinScore float32

	// RecentToSkip is the number of most recent messages to exclude from
	// vector search results (since they're already in the conversation).
	// Defaults to 10.
	RecentToSkip int

	// ContextHeader is the prefix added before injected context messages.
	// Defaults to "[Relevant context from earlier in the conversation]".
	ContextHeader string

	// Namespace for vector store isolation (e.g. per-session or per-user).
	Namespace string

	msgCounter atomic.Int64
}

// ContextualOption configures a ContextualMemory.
type ContextualOption func(*ContextualMemory)

// WithContextTopK sets how many relevant messages to retrieve.
func WithContextTopK(k int) ContextualOption {
	return func(c *ContextualMemory) { c.TopK = k }
}

// WithContextMinScore sets the minimum similarity score.
func WithContextMinScore(score float32) ContextualOption {
	return func(c *ContextualMemory) { c.MinScore = score }
}

// WithContextRecentToSkip sets how many recent messages to skip in retrieval.
func WithContextRecentToSkip(n int) ContextualOption {
	return func(c *ContextualMemory) { c.RecentToSkip = n }
}

// WithContextHeader sets the header for the injected context block.
func WithContextHeader(header string) ContextualOption {
	return func(c *ContextualMemory) { c.ContextHeader = header }
}

// WithContextNamespace sets the vector store namespace.
func WithContextNamespace(ns string) ContextualOption {
	return func(c *ContextualMemory) { c.Namespace = ns }
}

// NewContextualMemory creates a contextual memory that augments conversations
// with semantically relevant past messages retrieved from a vector store.
//
// Parameters:
//   - inner: the underlying memory (e.g. InMemoryMemory or SummarizingMemory)
//   - docStore: a document.DocumentStore configured with your embedding model and vector store
//   - opts: configuration options
//
// Example:
//
//	embedder := document.NewEmbedder(openaiEmbedder, 1536)
//	vstoreClient := vstore.NewClient(pgvectorProvider)
//	docStore := document.NewDocumentStore(vstoreClient, embedder).
//	    WithNamespace("conversation-memory")
//
//	base := memoryx.NewInMemoryMemory("You are a helpful assistant.")
//	mem := memoryx.NewContextualMemory(base, docStore,
//	    memoryx.WithContextTopK(5),
//	    memoryx.WithContextMinScore(0.7),
//	)
func NewContextualMemory(inner Memory, docStore *document.DocumentStore, opts ...ContextualOption) *ContextualMemory {
	cm := &ContextualMemory{
		inner:        inner,
		docStore:     docStore,
		TopK:         5,
		MinScore:     0.0,
		RecentToSkip: 10,
		ContextHeader: "[Relevant context from earlier in the conversation]",
	}
	for _, opt := range opts {
		opt(cm)
	}
	return cm
}

// Add stores the message in both the inner memory and the vector store.
func (c *ContextualMemory) Add(message llm.Message) error {
	if err := c.inner.Add(message); err != nil {
		return err
	}

	// Don't index system messages — they're always in context
	if message.Role == llm.RoleSystem {
		return nil
	}

	// Don't index empty messages
	content := c.messageToText(message)
	if content == "" {
		return nil
	}

	idx := c.msgCounter.Add(1)
	doc := document.NewDocument(content).
		WithID(fmt.Sprintf("msg-%d", idx)).
		WithMetadata("role", message.Role).
		WithMetadata("msg_index", idx).
		WithMetadata("type", "conversation_message")

	if message.ToolCallID != "" {
		doc.WithMetadata("tool_call_id", message.ToolCallID)
	}

	if len(message.ToolCalls) > 0 {
		names := make([]string, len(message.ToolCalls))
		for i, tc := range message.ToolCalls {
			names[i] = tc.Function.Name
		}
		doc.WithMetadata("tool_names", strings.Join(names, ","))
	}

	// Store in vector store — best-effort, don't fail the Add
	_ = c.docStore.AddDocuments(context.Background(), []*document.Document{doc})

	return nil
}

// Clear resets the inner memory (system prompt preserved).
// It does NOT clear the vector store — past context remains retrievable
// across conversation resets.
func (c *ContextualMemory) Clear() error {
	return c.inner.Clear()
}

// ClearAll resets both the inner memory and deletes all vectors in the namespace.
func (c *ContextualMemory) ClearAll(ctx context.Context) error {
	if err := c.inner.Clear(); err != nil {
		return err
	}

	// Collect all stored message IDs
	count := c.msgCounter.Load()
	if count == 0 {
		return nil
	}

	ids := make([]string, count)
	for i := int64(0); i < count; i++ {
		ids[i] = fmt.Sprintf("msg-%d", i+1)
	}

	c.msgCounter.Store(0)
	return c.docStore.DeleteDocuments(ctx, ids)
}

// Messages returns the conversation messages augmented with relevant context
// retrieved from the vector store.
//
// The returned message slice has the structure:
//
//	[system_prompt, context_message (if relevant hits found), ...conversation_messages]
func (c *ContextualMemory) Messages() ([]llm.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	messages, err := c.inner.Messages()
	if err != nil {
		return nil, err
	}

	// Need at least one non-system message to build a query
	conversation := messages
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		conversation = messages[1:]
	}
	if len(conversation) == 0 {
		return messages, nil
	}

	// Build a query from recent messages
	query := c.buildQuery(conversation)
	if query == "" {
		return messages, nil
	}

	// Retrieve relevant past messages
	retrieved, err := c.retrieveContext(query)
	if err != nil || len(retrieved) == 0 {
		return messages, nil
	}

	// Filter out messages that are already in the recent conversation
	recentSet := c.recentMessageSet(conversation)
	var relevant []string
	for _, doc := range retrieved {
		if !recentSet[doc.Content] {
			relevant = append(relevant, doc.Content)
		}
	}

	if len(relevant) == 0 {
		return messages, nil
	}

	// Build the context injection message
	contextContent := fmt.Sprintf("%s\n\n%s", c.ContextHeader, strings.Join(relevant, "\n\n"))
	contextMsg := llm.Message{
		Role:    llm.RoleUser,
		Content: contextContent,
		Metadata: map[string]any{
			"contextual_memory": true,
			"retrieved_count":   len(relevant),
		},
	}

	// Insert context after system prompt, before conversation
	result := make([]llm.Message, 0, len(messages)+1)
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		result = append(result, messages[0])  // system prompt
		result = append(result, contextMsg)   // injected context
		result = append(result, messages[1:]...) // conversation
	} else {
		result = append(result, contextMsg)   // injected context
		result = append(result, messages...)   // conversation
	}

	return result, nil
}

// buildQuery constructs a search query from the most recent messages.
func (c *ContextualMemory) buildQuery(conversation []llm.Message) string {
	// Take the last few messages as the query
	lookback := 4
	if lookback > len(conversation) {
		lookback = len(conversation)
	}

	recent := conversation[len(conversation)-lookback:]
	var parts []string
	for _, m := range recent {
		text := c.messageToText(m)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// retrieveContext searches the vector store for relevant past messages.
func (c *ContextualMemory) retrieveContext(query string) ([]*document.Document, error) {
	req := document.SearchRequest{
		Query:    query,
		TopK:     c.TopK + c.RecentToSkip, // fetch extra to account for skipping
		MinScore: c.MinScore,
	}
	if c.Namespace != "" {
		req.Namespace = c.Namespace
	}

	result, err := c.docStore.Search(context.Background(), req)
	if err != nil {
		return nil, err
	}

	// Skip the most recent messages (they're already in context)
	// We use msg_index to identify recency
	currentIdx := c.msgCounter.Load()
	threshold := currentIdx - int64(c.RecentToSkip)

	var filtered []*document.Document
	for _, doc := range result.Documents {
		idx, ok := doc.Metadata["msg_index"]
		if ok {
			var msgIdx int64
			switch v := idx.(type) {
			case int64:
				msgIdx = v
			case float64:
				msgIdx = int64(v)
			case json.Number:
				n, _ := v.Int64()
				msgIdx = n
			}
			if msgIdx > threshold {
				continue // skip recent
			}
		}
		filtered = append(filtered, doc)
		if len(filtered) >= c.TopK {
			break
		}
	}

	return filtered, nil
}

// recentMessageSet builds a set of recent message contents for deduplication.
func (c *ContextualMemory) recentMessageSet(conversation []llm.Message) map[string]bool {
	set := make(map[string]bool, len(conversation))
	for _, m := range conversation {
		text := c.messageToText(m)
		if text != "" {
			set[text] = true
		}
	}
	return set
}

// messageToText converts a message to a searchable text representation.
func (c *ContextualMemory) messageToText(m llm.Message) string {
	var parts []string

	if m.Content != "" {
		parts = append(parts, fmt.Sprintf("[%s]: %s", m.Role, m.Content))
	}

	for _, tc := range m.ToolCalls {
		parts = append(parts, fmt.Sprintf("[tool_call] %s(%s)", tc.Function.Name, tc.Function.Arguments))
	}

	return strings.Join(parts, "\n")
}
