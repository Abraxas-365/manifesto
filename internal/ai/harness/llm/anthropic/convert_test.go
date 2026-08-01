package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go"
)

var cc5m = anthropic.NewCacheControlEphemeralParam()

func cc1h() *anthropic.CacheControlEphemeralParam {
	cc := anthropic.NewCacheControlEphemeralParam()
	cc.TTL = anthropic.CacheControlEphemeralTTLTTL1h
	return &cc
}

// hasCacheControl checks if a content block has cache_control set.
func hasCacheControl(block anthropic.ContentBlockParamUnion) bool {
	cc := block.GetCacheControl()
	if cc == nil {
		return false
	}
	return cc.Type != ""
}

// hasCacheTTL1h checks if a content block's cache_control has 1h TTL.
func hasCacheTTL1h(block anthropic.ContentBlockParamUnion) bool {
	cc := block.GetCacheControl()
	if cc == nil {
		return false
	}
	return cc.TTL == anthropic.CacheControlEphemeralTTLTTL1h
}

// --- markMessageCacheControl pointer semantics ---

func TestMarkMessageCacheControl_MutatesOriginal(t *testing.T) {
	msg := anthropic.NewUserMessage(
		anthropic.NewTextBlock("hello"),
		anthropic.NewTextBlock("world"),
	)

	for i, b := range msg.Content {
		if hasCacheControl(b) {
			t.Fatalf("block %d has cache_control before marking", i)
		}
	}

	markMessageCacheControl(&msg, cc5m)

	if !hasCacheControl(msg.Content[1]) {
		t.Error("last block should have cache_control after marking")
	}
	if hasCacheControl(msg.Content[0]) {
		t.Error("first block should not have cache_control")
	}
}

func TestMarkMessageCacheControl_ToolUseBlock(t *testing.T) {
	msg := anthropic.NewAssistantMessage(
		anthropic.NewTextBlock("thinking..."),
		anthropic.NewToolUseBlock("id1", map[string]any{"x": 1}, "Read"),
	)

	markMessageCacheControl(&msg, cc5m)

	if !hasCacheControl(msg.Content[1]) {
		t.Error("tool_use block should have cache_control")
	}
}

func TestMarkMessageCacheControl_ToolResultBlock(t *testing.T) {
	msg := anthropic.NewUserMessage(
		anthropic.NewToolResultBlock("id1", "result text", false),
	)

	markMessageCacheControl(&msg, cc5m)

	if !hasCacheControl(msg.Content[0]) {
		t.Error("tool_result block should have cache_control")
	}
}

func TestMarkMessageCacheControl_EmptyMessage(t *testing.T) {
	msg := anthropic.MessageParam{
		Role:    "user",
		Content: nil,
	}
	markMessageCacheControl(&msg, cc5m) // must not panic
}

func TestMarkMessageCacheControl_ImageBlock(t *testing.T) {
	msg := anthropic.NewUserMessage(
		anthropic.NewImageBlockBase64("image/png", "base64data"),
	)

	markMessageCacheControl(&msg, cc5m)

	if !hasCacheControl(msg.Content[0]) {
		t.Error("image block should have cache_control")
	}
}

func TestMarkMessageCacheControl_TTL1h(t *testing.T) {
	msg := anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))
	markMessageCacheControl(&msg, *cc1h())
	if !hasCacheTTL1h(msg.Content[0]) {
		t.Error("cache_control should have 1h TTL")
	}
}

// --- toAnthropicMessages caching ---

func makeMessages(n int) []llm.Message {
	var msgs []llm.Message
	for i := 0; i < n; i++ {
		role := llm.RoleUser
		if i%2 == 1 {
			role = llm.RoleAssistant
		}
		msgs = append(msgs, llm.Message{
			Role:    role,
			Content: []llm.ContentBlock{llm.Text("message " + string(rune('A'+i)))},
		})
	}
	return msgs
}

func countCacheBreakpoints(msgs []anthropic.MessageParam) int {
	count := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if hasCacheControl(b) {
				count++
			}
		}
	}
	return count
}

func TestToAnthropicMessages_CachingDisabled_NoBreakpoints(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(6), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 0 {
		t.Errorf("caching=nil: got %d breakpoints, want 0", n)
	}
}

func TestToAnthropicMessages_CachingEnabled_SingleMessage(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(1), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 0 {
		t.Errorf("single message: got %d breakpoints, want 0", n)
	}
}

func TestToAnthropicMessages_CachingEnabled_TwoMessages(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(2), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 1 {
		t.Errorf("two messages: got %d breakpoints, want 1", n)
	}
	if !hasCacheControl(result[0].Content[0]) {
		t.Error("breakpoint should be on first message (second-to-last)")
	}
}

func TestToAnthropicMessages_CachingEnabled_ShortConversation(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(6), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 1 {
		t.Errorf("6 messages: got %d breakpoints, want 1", n)
	}
	if !hasCacheControl(result[4].Content[0]) {
		t.Error("breakpoint should be on message[4] (second-to-last)")
	}
}

func TestToAnthropicMessages_CachingEnabled_LongConversation(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(12), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 2 {
		t.Errorf("12 messages: got %d breakpoints, want 2", n)
	}
	if !hasCacheControl(result[10].Content[0]) {
		t.Error("breakpoint should be on message[10] (second-to-last)")
	}
	if !hasCacheControl(result[4].Content[0]) {
		t.Error("breakpoint should be on message[4] (midpoint)")
	}
}

func TestToAnthropicMessages_CachingEnabled_MaxBreakpoints(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(100), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	n := countCacheBreakpoints(result)
	if n > 2 {
		t.Errorf("message breakpoints should be at most 2, got %d", n)
	}
	if n != 2 {
		t.Errorf("100 messages: got %d breakpoints, want 2", n)
	}
}

func TestToAnthropicMessages_CachingEnabled_MidpointNotOverlap(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(10), &cc5m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countCacheBreakpoints(result); n != 2 {
		t.Errorf("10 messages: got %d breakpoints, want 2", n)
	}
	if !hasCacheControl(result[8].Content[0]) {
		t.Error("breakpoint should be on message[8] (second-to-last)")
	}
	if !hasCacheControl(result[3].Content[0]) {
		t.Error("breakpoint should be on message[3] (midpoint 10/3)")
	}
}

func TestToAnthropicMessages_TTL1h_Propagated(t *testing.T) {
	result, err := toAnthropicMessages(makeMessages(4), cc1h())
	if err != nil {
		t.Fatal(err)
	}
	if !hasCacheTTL1h(result[2].Content[0]) {
		t.Error("message breakpoint should have 1h TTL")
	}
}

// --- empty text block filtering ---

func TestToContentBlocks_SkipsEmptyText(t *testing.T) {
	msg := llm.Message{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.BlockText, Text: ""},
			{Type: llm.BlockImage, MediaType: "image/png", Data: "abc"},
		},
	}
	blocks := toContentBlocks(msg)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (image only), got %d", len(blocks))
	}
	if blocks[0].OfImage == nil {
		t.Error("expected image block")
	}
}

// --- system and tool caching ---

func TestExtractSystem_CachingEnabled(t *testing.T) {
	blocks := extractSystem("hello system", &cc5m)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(blocks))
	}
	if blocks[0].CacheControl.Type != "ephemeral" {
		t.Error("system block should have cache_control when caching enabled")
	}
}

func TestExtractSystem_CachingDisabled(t *testing.T) {
	blocks := extractSystem("hello system", nil)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(blocks))
	}
	if blocks[0].CacheControl.Type != "" {
		t.Error("system block should NOT have cache_control when caching disabled")
	}
}

func TestExtractSystem_TTL1h(t *testing.T) {
	blocks := extractSystem("hello system", cc1h())
	if blocks[0].CacheControl.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Error("system block should have 1h TTL")
	}
}

func TestToAnthropicTools_CachingEnabled(t *testing.T) {
	tools := []llm.ToolDef{
		{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"properties":{"path":{"type":"string"}},"required":["path"]}`)},
		{Name: "write", Description: "Write a file", InputSchema: json.RawMessage(`{"properties":{"path":{"type":"string"}},"required":["path"]}`)},
	}
	result := toAnthropicTools(tools, &cc5m)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].OfTool.CacheControl.Type != "" {
		t.Error("first tool should NOT have cache_control")
	}
	if result[1].OfTool.CacheControl.Type != "ephemeral" {
		t.Error("last tool should have cache_control when caching enabled")
	}
}

func TestToAnthropicTools_CachingDisabled(t *testing.T) {
	tools := []llm.ToolDef{
		{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"properties":{}}`)},
	}
	result := toAnthropicTools(tools, nil)
	if result[0].OfTool.CacheControl.Type != "" {
		t.Error("tool should NOT have cache_control when caching disabled")
	}
}

func TestToAnthropicTools_TTL1h(t *testing.T) {
	tools := []llm.ToolDef{
		{Name: "read", Description: "Read", InputSchema: json.RawMessage(`{"properties":{}}`)},
	}
	result := toAnthropicTools(tools, cc1h())
	if result[0].OfTool.CacheControl.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Error("tool should have 1h TTL")
	}
}

// --- full buildParams cache breakpoint budget ---

func TestBuildParams_TotalBreakpointsWithinLimit(t *testing.T) {
	p := &Provider{apiKey: "k", promptCaching: true}
	tools := []llm.ToolDef{
		{Name: "read", Description: "Read", InputSchema: json.RawMessage(`{"properties":{}}`)},
	}
	req := llm.Request{
		Model:    "claude-sonnet-4-20250514",
		System:   "You are a helpful assistant.",
		Messages: makeMessages(20),
		Tools:    tools,
	}
	params, err := p.buildParams(req)
	if err != nil {
		t.Fatal(err)
	}

	total := 0
	for _, sb := range params.System {
		if sb.CacheControl.Type != "" {
			total++
		}
	}
	for _, tb := range params.Tools {
		if tb.OfTool != nil && tb.OfTool.CacheControl.Type != "" {
			total++
		}
	}
	total += countCacheBreakpoints(params.Messages)

	if total > 4 {
		t.Errorf("total cache breakpoints = %d, want <= 4", total)
	}
	if total != 4 {
		t.Errorf("total cache breakpoints = %d, want 4", total)
	}
}

// --- TTL resolution ---

func TestResolveCacheTTL_EnvOverridesConfig(t *testing.T) {
	t.Setenv("CLAUDIO_CACHE_TTL", "5m")
	p := &Provider{cacheTTLSetting: "1h"}
	p.resolveCacheTTL(true) // OAuth → would auto-detect 1h, but env says 5m
	if p.cacheTTL1h.Load() {
		t.Error("env=5m should override config=1h")
	}
}

func TestResolveCacheTTL_ConfigOverridesAutoDetect(t *testing.T) {
	t.Setenv("CLAUDIO_CACHE_TTL", "")
	p := &Provider{cacheTTLSetting: "5m"}
	p.resolveCacheTTL(true) // OAuth → auto-detect would be 1h
	if p.cacheTTL1h.Load() {
		t.Error("config=5m should override auto-detect=1h")
	}
}

func TestResolveCacheTTL_AutoDetectOAuth(t *testing.T) {
	t.Setenv("CLAUDIO_CACHE_TTL", "")
	p := &Provider{}
	p.resolveCacheTTL(true)
	if !p.cacheTTL1h.Load() {
		t.Error("OAuth should auto-detect to 1h")
	}
}

func TestResolveCacheTTL_AutoDetectAPIKey(t *testing.T) {
	t.Setenv("CLAUDIO_CACHE_TTL", "")
	p := &Provider{}
	p.resolveCacheTTL(false)
	if p.cacheTTL1h.Load() {
		t.Error("API key should auto-detect to 5m")
	}
}

func TestResolveCacheTTL_LatchedOnce(t *testing.T) {
	t.Setenv("CLAUDIO_CACHE_TTL", "")
	p := &Provider{}
	p.resolveCacheTTL(true)  // latches to 1h
	p.resolveCacheTTL(false) // should not change
	if !p.cacheTTL1h.Load() {
		t.Error("TTL should remain latched at 1h after second call")
	}
}

func TestCacheControl_5m(t *testing.T) {
	p := &Provider{}
	p.cacheTTL1h.Store(false)
	cc := p.cacheControl()
	if cc.TTL != "" {
		t.Errorf("5m cache control should have empty TTL, got %q", cc.TTL)
	}
}

func TestCacheControl_1h(t *testing.T) {
	p := &Provider{}
	p.cacheTTL1h.Store(true)
	cc := p.cacheControl()
	if cc.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Errorf("1h cache control should have TTL=1h, got %q", cc.TTL)
	}
}

// --- TTL downgrade ---

func TestDowngradeCacheTTL(t *testing.T) {
	p := &Provider{}
	p.cacheTTL1h.Store(true)
	p.downgradeCacheTTL()
	if p.cacheTTL1h.Load() {
		t.Error("after downgrade, TTL should be 5m")
	}
	if !p.cacheTTLDowngraded.Load() {
		t.Error("downgraded flag should be set")
	}
}

// --- NewWithCredentials enables caching ---

func TestNewWithCredentials_CachingEnabled(t *testing.T) {
	creds := &fakeCreds{token: "k", isOAuth: false}
	p := NewWithCredentials(creds)
	if !p.promptCaching {
		t.Error("NewWithCredentials should enable prompt caching by default")
	}
}

func TestToContentBlocks_NonObjectToolUseInputWrapped(t *testing.T) {
	cases := []struct {
		name  string
		input json.RawMessage
	}{
		{"string", json.RawMessage(`"truncated input"`)},
		{"array", json.RawMessage(`[1,2]`)},
		{"number", json.RawMessage(`42`)},
		{"empty", nil},
		{"invalid", json.RawMessage(`{broken`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ToolUseBlock("t1", "Read", tc.input),
			}}
			blocks := toContentBlocks(msg)
			if len(blocks) != 1 || blocks[0].OfToolUse == nil {
				t.Fatalf("expected one tool_use block, got %+v", blocks)
			}
			// The API requires input to be a JSON object.
			data, err := json.Marshal(blocks[0].OfToolUse.Input)
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}
			var obj map[string]any
			if err := json.Unmarshal(data, &obj); err != nil {
				t.Fatalf("tool_use.input is not an object: %s", data)
			}
		})
	}
}
