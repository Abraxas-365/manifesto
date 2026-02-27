package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/ai/document"
	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/agentx"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/memoryx"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/toolx"
	"github.com/Abraxas-365/manifesto/pkg/ai/providers/aiopenai"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore/providers/vstmemory"
)

func main() {
	ctx := context.Background()

	fmt.Println("AI Package Examples")
	fmt.Println(strings.Repeat("=", 60))

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("Set OPENAI_API_KEY to run this example")
	}

	provider := aiopenai.NewOpenAIProvider(apiKey)

	exampleBasicChat(ctx, provider)
	exampleStreaming(ctx, provider)
	exampleAgent(ctx, provider)
	exampleMemorySummarization(ctx, provider)
	exampleContextualMemory(ctx, provider)
	exampleComposedMemory(ctx, provider)

	fmt.Println("\nDone!")
}

// ============================================================================
// 1. Basic Chat
// ============================================================================

func exampleBasicChat(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- Basic Chat ---")

	client := llm.NewClient(provider)

	messages := []llm.Message{
		llm.NewSystemMessage("You are a concise assistant. Reply in one sentence."),
		llm.NewUserMessage("What is Go good for?"),
	}

	resp, err := client.Chat(ctx, messages,
		llm.WithModel("gpt-4o-mini"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", resp.Message.Content)
	fmt.Printf("Tokens: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}

// ============================================================================
// 2. Streaming Chat
// ============================================================================

func exampleStreaming(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- Streaming ---")

	client := llm.NewClient(provider)

	messages := []llm.Message{
		llm.NewSystemMessage("You are a concise assistant."),
		llm.NewUserMessage("Count from 1 to 5, one per line."),
	}

	stream, err := client.ChatStream(ctx, messages, llm.WithModel("gpt-4o-mini"))
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	fmt.Print("Streaming: ")
	for {
		chunk, err := stream.Next()
		if err != nil {
			break // io.EOF
		}
		fmt.Print(chunk.Content)
	}
	fmt.Println()
}

// ============================================================================
// 3. Agent with Tools
// ============================================================================

// calculatorTool implements toolx.Toolx for the example.
type calculatorTool struct{}

func (t *calculatorTool) Name() string { return "calculator" }

func (t *calculatorTool) GetTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.Function{
			Name:        "calculator",
			Description: "Performs basic arithmetic. Input: JSON with 'expression' field.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Math expression like '2 + 3'",
					},
				},
				"required": []string{"expression"},
			},
		},
	}
}

func (t *calculatorTool) Call(ctx context.Context, input string) (any, error) {
	// Simplified — in production, parse the JSON input and evaluate
	return "Result: 42", nil
}

func exampleAgent(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- Agent with Tools ---")

	client := llm.NewClient(provider)
	memory := memoryx.NewInMemoryMemory("You are a helpful assistant with access to tools.")

	tools := toolx.FromToolx(&calculatorTool{})

	agent := agentx.New(*client, memory,
		agentx.WithTools(tools),
		agentx.WithOptions(llm.WithModel("gpt-4o-mini")),
		agentx.WithMaxAutoIterations(3),
	)

	// Simple conversation
	response, err := agent.Run(ctx, "What is 6 * 7? Use the calculator.")
	if err != nil {
		log.Printf("Agent error: %v", err)
		return
	}
	fmt.Printf("Agent: %s\n", response)

	// Streaming with tool events
	fmt.Println("\n--- Agent Streaming with Tools ---")

	agent2 := agentx.New(*client, memoryx.NewInMemoryMemory("You are helpful."),
		agentx.WithTools(tools),
		agentx.WithOptions(llm.WithModel("gpt-4o-mini")),
	)

	err = agent2.StreamWithTools(ctx, "Calculate 10 + 5 using the calculator.", func(event agentx.StreamEvent) {
		switch event.Type {
		case agentx.EventText:
			fmt.Print(event.Content)
		case agentx.EventToolCall:
			fmt.Printf("\n  [tool_call] %s(%s)\n", event.ToolName, event.ToolInput)
		case agentx.EventToolResult:
			fmt.Printf("  [tool_result] %s -> %s\n", event.ToolName, event.ToolOutput)
		case agentx.EventError:
			fmt.Printf("  [error] %v\n", event.Err)
		}
	})
	if err != nil {
		log.Printf("Stream error: %v", err)
	}
	fmt.Println()
}

// ============================================================================
// 4. SummarizingMemory — auto-compress when context gets large
// ============================================================================

func exampleMemorySummarization(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- SummarizingMemory ---")

	client := llm.NewClient(provider)

	// Create a base memory
	base := memoryx.NewInMemoryMemory("You are a helpful assistant.")

	// Wrap it with summarization — uses the same LLM to generate summaries
	memory := memoryx.NewSummarizingMemory(base, provider,
		memoryx.WithMaxTokens(2000),   // summarize when exceeding ~2000 tokens
		memoryx.WithRecentToKeep(4),   // always keep last 4 messages verbatim
		memoryx.WithOnSummarize(func(count int, summary string) {
			fmt.Printf("  [Summarized %d messages]\n", count)
			fmt.Printf("  Summary: %s\n", truncate(summary, 100))
		}),
		// Use a cheaper model for summarization
		memoryx.WithSummarizationOptions(llm.WithModel("gpt-4o-mini")),
	)

	agent := agentx.New(*client, memory,
		agentx.WithOptions(llm.WithModel("gpt-4o-mini")),
	)

	// Simulate a multi-turn conversation
	turns := []string{
		"What is Go?",
		"What are goroutines?",
		"How do channels work?",
		"What is a mutex?",
		"Explain the sync package.",
		"What about context.Context?",
	}

	for _, turn := range turns {
		fmt.Printf("\nUser: %s\n", turn)
		resp, err := agent.Run(ctx, turn)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}
		fmt.Printf("Assistant: %s\n", truncate(resp, 120))
	}

	// Check how many messages are in memory after summarization
	msgs, _ := memory.Messages()
	fmt.Printf("\nMessages in memory: %d (some may have been summarized)\n", len(msgs))
}

// ============================================================================
// 5. ContextualMemory — semantic retrieval from vector store
// ============================================================================

func exampleContextualMemory(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- ContextualMemory ---")

	client := llm.NewClient(provider)

	// Setup vector store + embedder
	memStore := vstmemory.NewMemoryVectorStore(1536, vstore.MetricCosine)
	vstoreClient := vstore.NewClient(memStore)
	embedder := document.NewEmbedder(provider, 1536, embedding.WithModel("text-embedding-3-small"))
	docStore := document.NewDocumentStore(vstoreClient, embedder)

	// Create contextual memory
	base := memoryx.NewInMemoryMemory("You are a helpful assistant with long-term memory.")
	memory := memoryx.NewContextualMemory(base, docStore,
		memoryx.WithContextTopK(3),
		memoryx.WithContextMinScore(0.5),
		memoryx.WithContextRecentToSkip(4), // skip last 4 msgs (already in context)
	)

	agent := agentx.New(*client, memory,
		agentx.WithOptions(llm.WithModel("gpt-4o-mini")),
	)

	// Have a conversation — early messages get stored in the vector store
	earlyTopics := []string{
		"My favorite programming language is Rust.",
		"I work at a startup called TechFlow.",
		"Our main product is an API gateway written in Go.",
	}

	for _, msg := range earlyTopics {
		fmt.Printf("User: %s\n", msg)
		resp, err := agent.Run(ctx, msg)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}
		fmt.Printf("Assistant: %s\n\n", truncate(resp, 100))
	}

	// Later, ask something that requires recalling earlier context
	fmt.Println("--- Later in the conversation ---")
	resp, err := agent.Run(ctx, "What language is our main product written in?")
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}
	fmt.Printf("User: What language is our main product written in?\n")
	fmt.Printf("Assistant: %s\n", resp)
}

// ============================================================================
// 6. Composed Memory — Summarization + Contextual together
// ============================================================================

func exampleComposedMemory(ctx context.Context, provider *aiopenai.OpenAIProvider) {
	fmt.Println("\n--- Composed Memory (Summarizing + Contextual) ---")

	client := llm.NewClient(provider)

	// Vector store setup
	memStore := vstmemory.NewMemoryVectorStore(1536, vstore.MetricCosine)
	vstoreClient := vstore.NewClient(memStore)
	embedder := document.NewEmbedder(provider, 1536, embedding.WithModel("text-embedding-3-small"))
	docStore := document.NewDocumentStore(vstoreClient, embedder)

	// Stack the layers:
	// Layer 1: Base in-memory storage
	base := memoryx.NewInMemoryMemory("You are a helpful assistant.")

	// Layer 2: Auto-summarize when context grows
	summarized := memoryx.NewSummarizingMemory(base, provider,
		memoryx.WithMaxTokens(4000),
		memoryx.WithRecentToKeep(6),
		memoryx.WithSummarizationOptions(llm.WithModel("gpt-4o-mini")),
		memoryx.WithOnSummarize(func(count int, _ string) {
			fmt.Printf("  [Summarized %d old messages]\n", count)
		}),
	)

	// Layer 3: Augment with vector-retrieved context
	memory := memoryx.NewContextualMemory(summarized, docStore,
		memoryx.WithContextTopK(3),
		memoryx.WithContextMinScore(0.5),
	)

	agent := agentx.New(*client, memory,
		agentx.WithOptions(llm.WithModel("gpt-4o-mini")),
	)

	fmt.Println("Memory stack: InMemory -> SummarizingMemory -> ContextualMemory")
	fmt.Println("This gives you: recent messages + compressed summaries + semantic retrieval")

	resp, err := agent.Run(ctx, "Hello! Tell me about yourself.")
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}
	fmt.Printf("Assistant: %s\n", truncate(resp, 120))
}

// ============================================================================
// Helpers
// ============================================================================

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
