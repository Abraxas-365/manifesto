package llm

// StopReason is a provider-agnostic reason the model stopped generating.
// Providers map their native reasons (Anthropic stop_reason, OpenAI
// finish_reason, ...) onto this closed set.
type StopReason string

const (
	// StopEndTurn: the model finished normally with a complete response.
	StopEndTurn StopReason = "end_turn"
	// StopToolUse: the model wants one or more tools executed.
	StopToolUse StopReason = "tool_use"
	// StopMaxTokens: generation hit the token limit before completing.
	StopMaxTokens StopReason = "max_tokens"
	// StopError: the provider ended abnormally (refusal, content filter, ...).
	StopError StopReason = "error"
	// StopContextWindowExceeded: the request exceeded the model's context
	// window. The agent loop should compact and retry.
	StopContextWindowExceeded StopReason = "model_context_window_exceeded"
	// StopUnknown: the provider reported a reason we do not recognize.
	StopUnknown StopReason = "unknown"
)

// NormalizeStopReason applies the defensive rule both opencode and pi rely on:
// if the assistant message contains tool_use blocks, the turn is a tool-use
// turn regardless of what the provider reported. Some providers (notably
// OpenAI) return "stop" even when tool calls are present.
func NormalizeStopReason(reason StopReason, msg Message) StopReason {
	if len(msg.ToolUses()) > 0 {
		return StopToolUse
	}
	return reason
}
