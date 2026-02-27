package memoryx_test

import (
	"context"
	"testing"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/memoryx"
)

// --- InMemoryMemory tests ---

func TestInMemoryMemory_Basic(t *testing.T) {
	m := memoryx.NewInMemoryMemory("You are helpful.")

	msgs, _ := m.Messages()
	if len(msgs) != 1 || msgs[0].Role != llm.RoleSystem {
		t.Fatalf("expected system prompt, got %d messages", len(msgs))
	}

	m.Add(llm.NewUserMessage("hello"))
	m.Add(llm.NewAssistantMessage("hi"))

	msgs, _ = m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestInMemoryMemory_ClearKeepsSystemPrompt(t *testing.T) {
	m := memoryx.NewInMemoryMemory("system")
	m.Add(llm.NewUserMessage("hello"))
	m.Clear()

	msgs, _ := m.Messages()
	if len(msgs) != 1 || msgs[0].Content != "system" {
		t.Fatalf("expected only system prompt after clear, got %+v", msgs)
	}
}

func TestInMemoryMemory_ClearNoSystemPrompt(t *testing.T) {
	m := memoryx.NewInMemoryMemory()
	m.Add(llm.NewUserMessage("hello"))
	m.Clear()

	msgs, _ := m.Messages()
	if len(msgs) != 0 {
		t.Fatalf("expected empty after clear, got %d", len(msgs))
	}
}

func TestInMemoryMemory_ReturnsDefensiveCopy(t *testing.T) {
	m := memoryx.NewInMemoryMemory()
	m.Add(llm.NewUserMessage("hello"))

	msgs1, _ := m.Messages()
	msgs1[0].Content = "mutated"

	msgs2, _ := m.Messages()
	if msgs2[0].Content != "hello" {
		t.Fatal("Messages() did not return a defensive copy")
	}
}

// --- TokenEstimator tests ---

func TestCharBasedEstimator(t *testing.T) {
	e := &memoryx.CharBasedEstimator{}
	msgs := []llm.Message{
		llm.NewUserMessage("hello world"), // 11 chars -> 2 + 4 overhead = 6
	}
	tokens := e.EstimateTokens(msgs)
	if tokens <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokens)
	}
}

// --- SummarizingMemory tests ---

// mockLLM is a fake LLM that returns a canned response.
type mockLLM struct {
	response string
	called   int
}

func (m *mockLLM) Chat(_ context.Context, messages []llm.Message, _ ...llm.Option) (llm.Response, error) {
	m.called++
	return llm.Response{Message: llm.NewAssistantMessage(m.response)}, nil
}

func (m *mockLLM) ChatStream(_ context.Context, _ []llm.Message, _ ...llm.Option) (llm.Stream, error) {
	return nil, nil
}

func TestSummarizingMemory_NoSummarizationUnderThreshold(t *testing.T) {
	base := memoryx.NewInMemoryMemory("system")
	mock := &mockLLM{response: "summary"}

	sm := memoryx.NewSummarizingMemory(base, mock,
		memoryx.WithMaxTokens(100000), // very high threshold
	)

	sm.Add(llm.NewUserMessage("hello"))
	sm.Add(llm.NewAssistantMessage("hi"))

	msgs, err := sm.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if mock.called != 0 {
		t.Fatal("LLM should not have been called under threshold")
	}
	if len(msgs) != 3 { // system + user + assistant
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestSummarizingMemory_SummarizesWhenOverThreshold(t *testing.T) {
	base := memoryx.NewInMemoryMemory("system")
	mock := &mockLLM{response: "This is the summary of prior conversation."}

	var summarizedCount int
	var gotSummary string

	sm := memoryx.NewSummarizingMemory(base, mock,
		memoryx.WithMaxTokens(10), // very low threshold to force summarization
		memoryx.WithRecentToKeep(2),
		memoryx.WithOnSummarize(func(count int, summary string) {
			summarizedCount = count
			gotSummary = summary
		}),
	)

	// Add enough messages to exceed the tiny threshold
	sm.Add(llm.NewUserMessage("first message"))
	sm.Add(llm.NewAssistantMessage("first response"))
	sm.Add(llm.NewUserMessage("second message"))
	sm.Add(llm.NewAssistantMessage("second response"))
	sm.Add(llm.NewUserMessage("third message"))
	sm.Add(llm.NewAssistantMessage("third response"))

	msgs, err := sm.Messages()
	if err != nil {
		t.Fatal(err)
	}

	if mock.called != 1 {
		t.Fatalf("expected LLM to be called once for summarization, got %d", mock.called)
	}

	if summarizedCount == 0 {
		t.Fatal("OnSummarize callback was not invoked")
	}

	if gotSummary != "This is the summary of prior conversation." {
		t.Fatalf("unexpected summary: %s", gotSummary)
	}

	// Should have: system + summary + 2 recent
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages after summarization, got %d", len(msgs))
	}

	// First should be system
	if msgs[0].Role != llm.RoleSystem {
		t.Fatalf("expected system prompt first, got %s", msgs[0].Role)
	}

	// Second should be the summary
	if msgs[1].Metadata == nil || msgs[1].Metadata["summarized"] != true {
		t.Fatal("expected summary message with metadata")
	}

	// Last two should be the recent messages
	if msgs[2].Content != "third message" {
		t.Fatalf("expected 'third message', got %q", msgs[2].Content)
	}
	if msgs[3].Content != "third response" {
		t.Fatalf("expected 'third response', got %q", msgs[3].Content)
	}
}

func TestSummarizingMemory_ClearDelegatesToInner(t *testing.T) {
	base := memoryx.NewInMemoryMemory("system")
	mock := &mockLLM{response: "summary"}
	sm := memoryx.NewSummarizingMemory(base, mock)

	sm.Add(llm.NewUserMessage("hello"))
	sm.Clear()

	msgs, _ := sm.Messages()
	if len(msgs) != 1 || msgs[0].Role != llm.RoleSystem {
		t.Fatal("expected only system prompt after clear")
	}
}
