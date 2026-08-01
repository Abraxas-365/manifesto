package tool

import (
	"context"
	"encoding/json"
)

// ImageData holds a base64-encoded image attached to a tool result.
type ImageData struct {
	MediaType string // e.g. "image/jpeg"
	Data      string // base64-encoded bytes
}

// Result is the output of a tool execution.
type Result struct {
	Content string      `json:"content"`
	IsError bool        `json:"is_error,omitempty"`
	Images  []ImageData `json:"-"`
}

// Tool is a capability the agent can invoke.
type Tool interface {
	// Name returns the tool's unique identifier.
	Name() string
	// Description returns a human-readable description for the model.
	Description() string
	// InputSchema returns the JSON Schema for the tool's input.
	InputSchema() json.RawMessage
	// Execute runs the tool with the given JSON input.
	Execute(ctx context.Context, input json.RawMessage) (*Result, error)
	// IsReadOnly reports whether the tool only reads and never mutates state.
	IsReadOnly() bool
}
