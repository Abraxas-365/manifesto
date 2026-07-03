package llm

import (
	"encoding/json"
	"testing"
)

func TestNormalizeStopReason_ToolUseOverrides(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{
		Text("let me check"),
		ToolUseBlock("id1", "Read", json.RawMessage(`{}`)),
	}}

	// Even if the provider said end_turn, the presence of a tool_use block wins.
	if got := NormalizeStopReason(StopEndTurn, msg); got != StopToolUse {
		t.Fatalf("expected StopToolUse, got %q", got)
	}
}

func TestNormalizeStopReason_PassthroughWhenNoTools(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{Text("all done")}}

	cases := []StopReason{StopEndTurn, StopMaxTokens, StopError, StopUnknown}
	for _, want := range cases {
		if got := NormalizeStopReason(want, msg); got != want {
			t.Fatalf("reason %q mutated to %q with no tool blocks", want, got)
		}
	}
}
