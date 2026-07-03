package llm

import "context"

// Request is a single chat completion request.
// ReasoningLevel is a portable reasoning/thinking intensity knob. Each provider
// adapter maps it to its own mechanism (OpenAI reasoning_effort, Anthropic
// thinking budget) and silently omits it when the model does not support
// reasoning. The empty value requests no reasoning.
type ReasoningLevel string

const (
	ReasoningNone    ReasoningLevel = ""
	ReasoningMinimal ReasoningLevel = "minimal"
	ReasoningLow     ReasoningLevel = "low"
	ReasoningMedium  ReasoningLevel = "medium"
	ReasoningHigh    ReasoningLevel = "high"
)

type Request struct {
	System    string
	Model     string
	MaxTokens int
	// Temperature is applied only when non-nil and the model supports it.
	Temperature *float64
	// TopP (nucleus sampling) is applied only when non-nil.
	TopP     *float64
	Messages []Message
	Tools    []ToolDef

	// Reasoning requests reasoning/thinking effort in a provider-agnostic way.
	// Empty = no reasoning. Adapters clamp/omit it for models that can't reason.
	Reasoning ReasoningLevel

	// Provider carries provider-specific request options that have no portable
	// equivalent, keyed by provider id ("openai", "anthropic"). Each provider
	// adapter reads only its own key and merges the entries into the outgoing
	// request body. Empty = nothing applied.
	Provider map[string]map[string]any
}

// Response is a non-streaming chat completion result.
type Response struct {
	Message    Message
	StopReason StopReason
	Usage      Usage
}

// StreamEvent is a single incremental event from a streaming response.
type StreamEvent struct {
	// TextDelta holds incremental assistant text, if any.
	TextDelta string
	// Done is true on the terminal event.
	Done bool
	// Message holds the fully assembled message on the terminal event.
	Message Message
	// StopReason is set on the terminal event.
	StopReason StopReason
	// Usage is set on the terminal event.
	Usage Usage
}

// Stream is an iterator over streaming response events.
type Stream interface {
	// Next returns the next event. On completion it returns an event with
	// Done set to true.
	Next() (StreamEvent, error)
	Close() error
}

// Provider is an LLM backend that supports tool-calling conversations.
type Provider interface {
	Chat(ctx context.Context, req Request) (*Response, error)
	ChatStream(ctx context.Context, req Request) (Stream, error)
}
