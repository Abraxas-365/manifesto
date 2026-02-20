package agentx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/memoryx"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm/toolx"
)

// Agent represents an LLM-powered agent with memory and tool capabilities
type Agent struct {
	client             *llm.Client
	tools              *toolx.ToolxClient
	memory             memoryx.Memory
	options            []llm.Option
	maxAutoIterations  int // Max iterations with "auto" tool choice
	maxTotalIterations int // Hard limit to prevent infinite loops
}

// AgentOption configures an Agent
type AgentOption func(*Agent)

// WithOptions adds LLM options to the agent
func WithOptions(options ...llm.Option) AgentOption {
	return func(a *Agent) {
		a.options = append(a.options, options...)
	}
}

// WithTools adds tools to the agent
func WithTools(tools *toolx.ToolxClient) AgentOption {
	return func(a *Agent) {
		a.tools = tools
	}
}

// WithMaxAutoIterations sets the maximum number of "auto" tool choice iterations
func WithMaxAutoIterations(max int) AgentOption {
	return func(a *Agent) {
		a.maxAutoIterations = max
	}
}

// WithMaxTotalIterations sets the hard limit for total iterations
func WithMaxTotalIterations(max int) AgentOption {
	return func(a *Agent) {
		a.maxTotalIterations = max
	}
}

// New creates a new agent
func New(client llm.Client, memory memoryx.Memory, opts ...AgentOption) *Agent {
	agent := &Agent{
		client:             &client,
		memory:             memory,
		maxAutoIterations:  3,  // Default: 3 "auto" iterations
		maxTotalIterations: 10, // Hard limit for safety
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

// Run processes a user message and returns the final response
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	// Add user message to memory
	if err := a.memory.Add(llm.NewUserMessage(userInput)); err != nil {
		return "", fmt.Errorf("failed to add user message: %w", err)
	}

	// Get messages from memory
	messages, err := a.memory.Messages()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Check if tools are available and add them as options if so
	options := a.options
	if a.tools != nil {
		// Convert tools to LLM-compatible format
		toolList := a.getToolsList()
		if len(toolList) > 0 {
			options = append(options, llm.WithTools(toolList))
		}
	}

	// Get response from LLM
	response, err := a.client.Chat(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("LLM error: %w", err)
	}

	// Add the response to memory
	if err := a.memory.Add(response.Message); err != nil {
		return "", fmt.Errorf("failed to add assistant response: %w", err)
	}

	// Check if the response contains tool calls
	if len(response.Message.ToolCalls) > 0 && a.tools != nil {
		return a.handleToolCalls(ctx, response.Message.ToolCalls)
	}

	return response.Message.Content, nil
}

// RunStream streams the agent's initial response
// Note: This doesn't handle tool calls in streaming mode
func (a *Agent) RunStream(ctx context.Context, userInput string) (llm.Stream, error) {
	// Add user message to memory
	if err := a.memory.Add(llm.NewUserMessage(userInput)); err != nil {
		return nil, fmt.Errorf("failed to add user message: %w", err)
	}

	// Get messages from memory
	messages, err := a.memory.Messages()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Check if tools are available and add them as options if so
	options := a.options
	if a.tools != nil {
		toolList := a.getToolsList()
		if len(toolList) > 0 {
			options = append(options, llm.WithTools(toolList))
		}
	}

	// Get streaming response
	return a.client.ChatStream(ctx, messages, options...)
}

// handleToolCalls processes tool calls and returns the final response
func (a *Agent) handleToolCalls(ctx context.Context, toolCalls []llm.ToolCall) (string, error) {
	return a.handleToolCallsWithLimit(ctx, toolCalls, 0)
}

// handleToolCallsWithLimit processes tool calls with iteration limit
func (a *Agent) handleToolCallsWithLimit(ctx context.Context, toolCalls []llm.ToolCall, iteration int) (string, error) {
	// Hard limit check
	if iteration >= a.maxTotalIterations {
		return "", fmt.Errorf("maximum total iterations (%d) exceeded", a.maxTotalIterations)
	}

	// Process each tool call
	for _, tc := range toolCalls {
		// Call the tool
		toolResponse, err := a.tools.Call(ctx, tc)
		if err != nil {
			return "", fmt.Errorf("tool execution error: %w", err)
		}

		// Add tool response to memory
		if err := a.memory.Add(toolResponse); err != nil {
			return "", fmt.Errorf("failed to add tool response: %w", err)
		}
	}

	// Get messages from memory
	messages, err := a.memory.Messages()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Smart tool choice: "auto" for first maxAutoIterations, then "none"
	options := a.options
	if a.tools != nil {
		toolList := a.getToolsList()
		if len(toolList) > 0 {
			options = append(options, llm.WithTools(toolList))

			if iteration < a.maxAutoIterations {
				// First N iterations: allow "auto" tool calling
				options = append(options, llm.WithToolChoice("auto"))
			} else {
				// After N iterations: force "none" to prevent more tool calls
				options = append(options, llm.WithToolChoice("none"))
			}
		}
	}

	// Get next response from LLM with tool results
	response, err := a.client.Chat(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("LLM error: %w", err)
	}

	// Add the response to memory
	if err := a.memory.Add(response.Message); err != nil {
		return "", fmt.Errorf("failed to add assistant response: %w", err)
	}

	// Check if we have more tool calls to handle
	if len(response.Message.ToolCalls) > 0 {
		return a.handleToolCallsWithLimit(ctx, response.Message.ToolCalls, iteration+1)
	}

	return response.Message.Content, nil
}

// getToolsList converts the tools to LLM-compatible format
func (a *Agent) getToolsList() []llm.Tool {
	return a.tools.GetTools()
}

// ClearMemory resets the conversation but keeps the system prompt
func (a *Agent) ClearMemory() error {
	return a.memory.Clear()
}

// AddMessage adds a message to memory
func (a *Agent) AddMessage(message llm.Message) error {
	return a.memory.Add(message)
}

// Messages returns all messages in memory
func (a *Agent) Messages() ([]llm.Message, error) {
	return a.memory.Messages()
}

// StreamWithTools streams the full agent loop including tool calls.
// The handler receives structured StreamEvents so the caller can react to
// text chunks, tool invocations, and tool results independently.
func (a *Agent) StreamWithTools(ctx context.Context, userInput string, handler StreamHandler) error {
	if err := a.memory.Add(llm.NewUserMessage(userInput)); err != nil {
		return fmt.Errorf("failed to add user message: %w", err)
	}

	for iteration := 0; iteration < a.maxTotalIterations; iteration++ {
		messages, err := a.memory.Messages()
		if err != nil {
			return fmt.Errorf("failed to retrieve messages: %w", err)
		}

		// Build options — force tools off after maxAutoIterations
		options := a.buildOptions(iteration)

		// ── 1. Stream the LLM response ────────────────────────────────────
		stream, err := a.client.ChatStream(ctx, messages, options...)
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		assistantMsg, err := a.consumeStream(ctx, stream, handler)
		stream.Close()
		if err != nil {
			return err
		}

		// Persist the full assistant message (text + any tool_calls)
		if err := a.memory.Add(assistantMsg); err != nil {
			return fmt.Errorf("failed to add assistant message: %w", err)
		}

		// ── 2. No tool calls → we're done ─────────────────────────────────
		if len(assistantMsg.ToolCalls) == 0 {
			return nil
		}

		// ── 3. Execute tools, emit events for each ────────────────────────
		if a.tools == nil {
			return nil
		}

		if err := a.executeAndEmitTools(ctx, assistantMsg.ToolCalls, handler); err != nil {
			return err
		}

		// Loop: build new messages with tool results and call LLM again
	}

	return fmt.Errorf("maximum iterations (%d) exceeded", a.maxTotalIterations)
}

// consumeStream drains a Stream, forwards text chunks as EventText events,
// accumulates tool call deltas, and returns the fully-assembled Message.
func (a *Agent) consumeStream(ctx context.Context, stream llm.Stream, handler StreamHandler) (llm.Message, error) {
	var (
		contentBuf strings.Builder
		toolCalls  []llm.ToolCall // final snapshot from the stream's internal accumulator
	)

	for {
		chunk, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return llm.Message{}, fmt.Errorf("stream read error: %w", err)
		}

		// Forward text delta immediately
		if chunk.Content != "" {
			contentBuf.WriteString(chunk.Content)
			handler(StreamEvent{
				Type:    EventText,
				Content: chunk.Content,
			})
		}

		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   contentBuf.String(),
		ToolCalls: toolCalls,
	}, nil
}

// executeAndEmitTools runs every tool call sequentially, emits before/after events,
// and adds each result to memory so the next LLM call has full context.
func (a *Agent) executeAndEmitTools(ctx context.Context, toolCalls []llm.ToolCall, handler StreamHandler) error {
	for _, tc := range toolCalls {
		// Notify caller: tool is about to run
		handler(StreamEvent{
			Type:       EventToolCall,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			ToolInput:  tc.Function.Arguments,
		})

		// Execute
		toolMsg, err := a.tools.Call(ctx, tc)
		if err != nil {
			handler(StreamEvent{Type: EventError, Err: err})
			return fmt.Errorf("tool %q failed: %w", tc.Function.Name, err)
		}

		// Notify caller: tool finished
		handler(StreamEvent{
			Type:       EventToolResult,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			ToolOutput: toolMsg.Content,
		})

		// Persist result so the next LLM call sees it
		if err := a.memory.Add(toolMsg); err != nil {
			return fmt.Errorf("failed to add tool result: %w", err)
		}
	}
	return nil
}

// buildOptions constructs the LLM option slice for a given iteration.
// After maxAutoIterations it forces tool_choice=none to break the loop.
func (a *Agent) buildOptions(iteration int) []llm.Option {
	options := append([]llm.Option(nil), a.options...) // copy

	if a.tools == nil {
		return options
	}

	toolList := a.getToolsList()
	if len(toolList) == 0 {
		return options
	}

	options = append(options, llm.WithTools(toolList))

	if iteration >= a.maxAutoIterations {
		options = append(options, llm.WithToolChoice("none"))
	} else {
		options = append(options, llm.WithToolChoice("auto"))
	}

	return options
}

// mergeToolCallDelta assembles streaming tool-call deltas into complete ToolCalls.
// OpenAI streams tool call arguments across multiple chunks identified by index/ID.
func mergeToolCallDelta(existing []llm.ToolCall, delta llm.ToolCall) []llm.ToolCall {
	// If we already have a call with this ID, append to its arguments
	for i, tc := range existing {
		if tc.ID == delta.ID {
			existing[i].Function.Name += delta.Function.Name
			existing[i].Function.Arguments += delta.Function.Arguments
			return existing
		}
	}
	// New tool call — only start one if it has an ID (first delta for that call)
	if delta.ID != "" {
		return append(existing, llm.ToolCall{
			ID:   delta.ID,
			Type: "function",
			Function: llm.FunctionCall{
				Name:      delta.Function.Name,
				Arguments: delta.Function.Arguments,
			},
		})
	}
	// Delta without ID means it's appending to the last call (index-based streaming)
	if len(existing) > 0 {
		last := len(existing) - 1
		existing[last].Function.Name += delta.Function.Name
		existing[last].Function.Arguments += delta.Function.Arguments
	}
	return existing
}

// RunConversation runs a complete conversation with multiple turns
func (a *Agent) RunConversation(ctx context.Context, userInputs []string) ([]string, error) {
	var responses []string

	for _, input := range userInputs {
		response, err := a.Run(ctx, input)
		if err != nil {
			return responses, err
		}
		responses = append(responses, response)
	}

	return responses, nil
}

// EvaluateWithTools runs the agent with tools and returns detailed execution info
func (a *Agent) EvaluateWithTools(ctx context.Context, userInput string) (*AgentEvaluation, error) {
	eval := &AgentEvaluation{
		UserInput: userInput,
		Steps:     []AgentStep{},
	}

	// Add user message to memory
	if err := a.memory.Add(llm.NewUserMessage(userInput)); err != nil {
		return nil, fmt.Errorf("failed to add user message: %w", err)
	}

	// Start evaluation process
	evalStep := AgentStep{
		StepType: "initial",
	}

	// Get messages from memory
	messages, err := a.memory.Messages()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve messages: %w", err)
	}
	evalStep.InputMessages = messages

	// Check if tools are available and add them as options if so
	options := a.options
	if a.tools != nil {
		toolList := a.getToolsList()
		if len(toolList) > 0 {
			options = append(options, llm.WithTools(toolList))
		}
	}

	// Get response from LLM
	response, err := a.client.Chat(ctx, messages, options...)
	if err != nil {
		return nil, fmt.Errorf("LLM error: %w", err)
	}

	evalStep.OutputMessage = response.Message
	evalStep.TokenUsage = response.Usage
	eval.Steps = append(eval.Steps, evalStep)

	// Add the response to memory
	if err := a.memory.Add(response.Message); err != nil {
		return nil, fmt.Errorf("failed to add assistant response: %w", err)
	}

	// Check if the response contains tool calls
	if len(response.Message.ToolCalls) > 0 && a.tools != nil {
		result, steps, err := a.evaluateToolCalls(ctx, response.Message.ToolCalls)
		if err != nil {
			return nil, err
		}

		eval.Steps = append(eval.Steps, steps...)
		eval.FinalResponse = result
	} else {
		eval.FinalResponse = response.Message.Content
	}

	return eval, nil
}

// evaluateToolCalls processes tool calls and records evaluation steps
func (a *Agent) evaluateToolCalls(ctx context.Context, toolCalls []llm.ToolCall) (string, []AgentStep, error) {
	return a.evaluateToolCallsWithLimit(ctx, toolCalls, 0)
}

// evaluateToolCallsWithLimit processes tool calls with iteration limit
func (a *Agent) evaluateToolCallsWithLimit(ctx context.Context, toolCalls []llm.ToolCall, iteration int) (string, []AgentStep, error) {
	var steps []AgentStep

	// Hard limit check
	if iteration >= a.maxTotalIterations {
		return "", steps, fmt.Errorf("maximum total iterations (%d) exceeded", a.maxTotalIterations)
	}

	// Process each tool call
	toolStep := AgentStep{
		StepType:  "tool_execution",
		ToolCalls: toolCalls,
	}

	var toolResponses []llm.Message
	for _, tc := range toolCalls {
		// Call the tool
		toolResponse, err := a.tools.Call(ctx, tc)
		if err != nil {
			return "", steps, fmt.Errorf("tool execution error: %w", err)
		}

		toolResponses = append(toolResponses, toolResponse)

		// Add tool response to memory
		if err := a.memory.Add(toolResponse); err != nil {
			return "", steps, fmt.Errorf("failed to add tool response: %w", err)
		}
	}

	toolStep.ToolResponses = toolResponses
	steps = append(steps, toolStep)

	// Get messages from memory
	messages, err := a.memory.Messages()
	if err != nil {
		return "", steps, fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Get next response from LLM with tool results
	responseStep := AgentStep{
		StepType:      "response",
		InputMessages: messages,
	}

	options := a.options
	if a.tools != nil {
		toolList := a.getToolsList()
		if len(toolList) > 0 {
			options = append(options, llm.WithTools(toolList))

			if iteration < a.maxAutoIterations {
				// First N iterations: allow "auto" tool calling
				options = append(options, llm.WithToolChoice("auto"))
			} else {
				// After N iterations: force "none" to prevent more tool calls
				options = append(options, llm.WithToolChoice("none"))
			}
		}
	}

	response, err := a.client.Chat(ctx, messages, options...)
	if err != nil {
		return "", steps, fmt.Errorf("LLM error: %w", err)
	}

	responseStep.OutputMessage = response.Message
	responseStep.TokenUsage = response.Usage
	steps = append(steps, responseStep)

	// Add the response to memory
	if err := a.memory.Add(response.Message); err != nil {
		return "", steps, fmt.Errorf("failed to add assistant response: %w", err)
	}

	// Check if we have more tool calls to handle
	if len(response.Message.ToolCalls) > 0 {
		result, moreSteps, err := a.evaluateToolCallsWithLimit(ctx, response.Message.ToolCalls, iteration+1)
		if err != nil {
			return "", steps, err
		}

		steps = append(steps, moreSteps...)
		return result, steps, nil
	}

	return response.Message.Content, steps, nil
}

// Types for evaluation

type AgentEvaluation struct {
	UserInput     string      `json:"user_input"`
	Steps         []AgentStep `json:"steps"`
	FinalResponse string      `json:"final_response"`
}

type AgentStep struct {
	StepType      string         `json:"step_type"`      // "initial", "tool_execution", "response"
	InputMessages []llm.Message  `json:"input_message"`  // Messages sent to the LLM
	OutputMessage llm.Message    `json:"output_message"` // Response from the LLM
	ToolCalls     []llm.ToolCall `json:"tool_calls"`     // Tool calls made
	ToolResponses []llm.Message  `json:"tool_responses"` // Responses from the tools
	TokenUsage    llm.Usage      `json:"token_usage"`    // Token usage information
}
