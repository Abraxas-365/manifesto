package agentx

// StreamEventType identifies what kind of event is being emitted
type StreamEventType string

const (
	// EventText is a chunk of LLM response text
	EventText StreamEventType = "text"

	// EventToolCall fires when the agent decides to call a tool (before execution)
	EventToolCall StreamEventType = "tool_call"

	// EventToolResult fires after a tool has executed and returned a result
	EventToolResult StreamEventType = "tool_result"

	// EventError fires if something goes wrong mid-stream
	EventError StreamEventType = "error"
)

// StreamEvent is the structured payload sent to the caller on every stream tick
type StreamEvent struct {
	Type StreamEventType

	// EventText: the incremental text chunk from the LLM
	Content string

	// EventToolCall / EventToolResult
	ToolCallID string
	ToolName   string

	// EventToolCall: raw JSON arguments the LLM sent to the tool
	ToolInput string

	// EventToolResult: serialised result returned by the tool
	ToolOutput string

	// EventError
	Err error
}

// StreamHandler receives events as they happen
type StreamHandler func(event StreamEvent)
