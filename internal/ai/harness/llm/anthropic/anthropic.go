package anthropic

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is used when a request does not specify a model.
const DefaultModel = "claude-sonnet-4-20250514"

// DefaultMaxTokens is used when a request does not specify a token limit.
// This is the provider-level fallback — the agent layer sets its own default
// (DefaultAgentMaxTokens = 8192) with escalation, so this only fires for
// non-agent callers (e.g. sidequery, compaction).
const DefaultMaxTokens = 8192

// nonStreamingRequestTimeout bounds non-streaming Chat calls. Setting it
// explicitly makes the SDK skip its own duration estimate/rejection (see
// CalculateNonStreamingTimeout in the vendored client), which otherwise
// refuses any non-streaming request whose max_tokens implies >10 minutes.
const nonStreamingRequestTimeout = time.Hour

// Credentials is a per-request credential source (satisfied by
// pkg/auth.Credentials). isOAuth selects Bearer-token auth with the Claude
// Code beta headers; otherwise the token is sent as an x-api-key.
type Credentials interface {
	Token(ctx context.Context) (token string, isOAuth bool, err error)
	// HandleUnauthorized is called after a 401; returning true retries once.
	HandleUnauthorized(ctx context.Context) bool
}

// OAuth request constants (mirrors claudio legacy / Claude Code).
const (
	oauthBeta          = "claude-code-20250219,oauth-2025-04-20"
	oauthUserAgent     = "claude-cli/2.1.195 (external, sdk-cli)"
	oauthBillingSystem = "x-anthropic-billing-header: cc_version=2.1.195; cc_entrypoint=cli; cch=00000;"
)

// Provider implements llm.Provider using the Anthropic Messages API.
type Provider struct {
	client        anthropic.Client
	apiKey        string
	creds         Credentials
	promptCaching bool

	// thinkingClear tracks the cache-staleness latch for the clear_thinking
	// context-editing strategy. When the gap since the last successful completion
	// exceeds the prompt-cache TTL, the prefix cache is already cold, so the
	// next request keeps only the most recent thinking turn (shrinking the
	// payload) instead of "all". The latch is sticky for the session, matching
	// claude-code's getThinkingClearLatched/setThinkingClearLatched behavior.
	thinkingClearMu      sync.Mutex
	lastCompletionAt     time.Time
	thinkingClearLatched bool

	// cache TTL latch: resolved once per provider lifetime for session stability —
	// flipping TTLs mid-session would fragment the server-side prompt cache.
	// cacheTTLSetting is the config-provided preference ("5m"/"1h", "" = auto).
	// cacheTTL1h is atomic because downgradeCacheTTL may flip it after the
	// latch resolved (API rejected the 1h ttl) while other requests read it.
	cacheTTLOnce       sync.Once
	cacheTTL1h         atomic.Bool
	cacheTTLDowngraded atomic.Bool
	cacheTTLSetting    string
}

// Option configures a Provider.
type Option func(*Provider)

// WithPromptCaching enables Anthropic prompt caching by injecting cache_control
// breakpoints on the system prompt and tool definitions.
func WithPromptCaching() Option {
	return func(p *Provider) { p.promptCaching = true }
}

// New creates an Anthropic provider. If apiKey is empty it falls back to the
// ANTHROPIC_API_KEY environment variable.
func New(apiKey string, opts ...option.RequestOption) *Provider {
	return NewWithOptions(apiKey, nil, opts...)
}

// NewWithOptions creates an Anthropic provider with harness-level options (such
// as prompt caching) in addition to SDK request options. If apiKey is empty it
// falls back to the ANTHROPIC_API_KEY environment variable.
func NewWithOptions(apiKey string, cacheOpts []Option, sdkOpts ...option.RequestOption) *Provider {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, sdkOpts...)
	client := anthropic.NewClient(options...)

	p := &Provider{client: client, apiKey: apiKey}
	for _, opt := range cacheOpts {
		opt(p)
	}
	return p
}

// NewWithCredentials creates a provider whose auth is resolved per request
// from creds — API key or OAuth (Claude Pro/Max), with auto-refresh handled
// by the credential source. Prompt caching is always enabled for credential-
// based providers (interactive sessions benefit unconditionally).
func NewWithCredentials(creds Credentials, sdkOpts ...option.RequestOption) *Provider {
	client := anthropic.NewClient(sdkOpts...)
	return &Provider{client: client, creds: creds, promptCaching: true}
}

// authOpts resolves per-request auth options. For OAuth it swaps the API key
// for a Bearer token plus the Claude Code beta/attribution headers.
func (p *Provider) authOpts(ctx context.Context) (opts []option.RequestOption, isOAuth bool, err error) {
	if p.creds == nil {
		return nil, false, nil
	}
	token, isOAuth, err := p.creds.Token(ctx)
	if err != nil {
		return nil, false, llm.Registry.NewWithCause(llm.ErrMissingAPIKey, err)
	}
	if !isOAuth {
		return []option.RequestOption{option.WithAPIKey(token)}, false, nil
	}
	return []option.RequestOption{
		option.WithAuthToken(token),
		option.WithHeader("anthropic-beta", oauthBeta),
		option.WithHeader("User-Agent", oauthUserAgent),
		option.WithHeader("anthropic-dangerous-direct-browser-access", "true"),
		option.WithHeader("x-app", "cli"),
	}, true, nil
}

// withBilling prepends the OAuth billing attribution block to the system
// prompt (required for subscription-billed requests, mirrors legacy).
func withBilling(params anthropic.MessageNewParams) anthropic.MessageNewParams {
	block := anthropic.TextBlockParam{Text: oauthBillingSystem}
	params.System = append([]anthropic.TextBlockParam{block}, params.System...)
	return params
}

// SetCacheTTL configures the preferred cache TTL. Must be called before the
// first API request (typically during init from Lua config). Valid values:
// "5m", "1h", "" (auto: OAuth→1h, API-key→5m).
func (p *Provider) SetCacheTTL(ttl string) { p.cacheTTLSetting = ttl }

// CacheTTLWasDowngraded reports whether a 1h TTL was auto-downgraded to 5m
// after the API rejected it (used by the TUI to show a diagnostic).
func (p *Provider) CacheTTLWasDowngraded() bool { return p.cacheTTLDowngraded.Load() }

// resolveCacheTTL latches the cache TTL on first use. Resolution order:
//  1. CLAUDIO_CACHE_TTL env var
//  2. Lua config cache_ttl setting
//  3. Auto: OAuth→1h (no extra cost on subscriptions), API-key→5m
func (p *Provider) resolveCacheTTL(isOAuth bool) {
	p.cacheTTLOnce.Do(func() {
		for _, pref := range []string{os.Getenv("CLAUDIO_CACHE_TTL"), p.cacheTTLSetting} {
			switch strings.ToLower(strings.TrimSpace(pref)) {
			case "1h":
				p.cacheTTL1h.Store(true)
				return
			case "5m":
				return // 5m is the default (no TTL field needed)
			}
		}
		// Auto-detect: OAuth subscriptions get 1h at no extra cost; API-key
		// users pay 2x for 1h cache writes vs 1.25x for 5m.
		p.cacheTTL1h.Store(isOAuth)
	})
}

// cacheControl returns the CacheControlEphemeralParam with the resolved TTL.
// Must be called after resolveCacheTTL.
func (p *Provider) cacheControl() anthropic.CacheControlEphemeralParam {
	cc := anthropic.NewCacheControlEphemeralParam()
	if p.cacheTTL1h.Load() {
		cc.TTL = anthropic.CacheControlEphemeralTTLTTL1h
	}
	return cc
}

// isTTLRejection reports whether an API error is a 400 rejecting the 1h TTL.
func (p *Provider) isTTLRejection(err error) bool {
	if !p.cacheTTL1h.Load() {
		return false
	}
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		return false
	}
	body := strings.ToLower(apiErr.RawJSON())
	return strings.Contains(body, "ttl") || strings.Contains(body, "cache_control")
}

// downgradeCacheTTL switches from 1h to 5m after the API rejected 1h. The
// next prepare() call will emit cache_control without TTL and drop the beta
// header. This is permanent for the session — no flip-flopping.
func (p *Provider) downgradeCacheTTL() {
	p.cacheTTL1h.Store(false)
	p.cacheTTLDowngraded.Store(true)
}

// cacheTTLDuration returns the effective cache TTL duration.
func (p *Provider) cacheTTLDuration() time.Duration {
	if p.cacheTTL1h.Load() {
		return time.Hour
	}
	return 5 * time.Minute
}

// thinkingClearActive reports whether the clear_thinking latch is set, flipping
// it on if the gap since the last successful completion exceeds the cache TTL.
// Once set it stays set for the lifetime of the provider (session-sticky),
// mirroring claude-code's getThinkingClearLatched/setThinkingClearLatched.
func (p *Provider) thinkingClearActive() bool {
	p.thinkingClearMu.Lock()
	defer p.thinkingClearMu.Unlock()
	if !p.thinkingClearLatched && !p.lastCompletionAt.IsZero() &&
		time.Since(p.lastCompletionAt) > p.cacheTTLDuration() {
		p.thinkingClearLatched = true
	}
	return p.thinkingClearLatched
}

// recordCompletion stamps the time of a successful API completion. It feeds the
// clear_thinking cache-staleness latch (see thinkingClearActive).
func (p *Provider) recordCompletion() {
	p.thinkingClearMu.Lock()
	p.lastCompletionAt = time.Now()
	p.thinkingClearMu.Unlock()
}

// contextManagementOpts returns SDK options that inject the context_management
// field into the API request. This enables server-side thinking block management
// (clear_thinking_20251015) which lets the server efficiently drop/keep thinking
// blocks without the client needing to re-send them all. Matches legacy's
// applyContextManagement behavior.
func (p *Provider) contextManagementOpts(params anthropic.MessageNewParams) []option.RequestOption {
	// Only apply when thinking is enabled (fixed budget or adaptive).
	if params.Thinking.OfEnabled == nil && params.Thinking.OfAdaptive == nil {
		return nil
	}

	keep := map[string]any{"type": "all"}
	if p.thinkingClearActive() {
		keep = map[string]any{"type": "thinking_turns", "value": 1}
	}

	edits := []map[string]any{{
		"type": "clear_thinking_20251015",
		"keep": keep,
	}}

	return []option.RequestOption{
		option.WithJSONSet("context_management", map[string]any{"edits": edits}),
	}
}

// isUnauthorized reports whether err is an HTTP 401 from the API.
func isUnauthorized(err error) bool {
	var apiErr *anthropic.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == 401
}

func (p *Provider) buildParams(req llm.Request) (anthropic.MessageNewParams, error) {
	// Resolve the cache_control value: nil → no caching, non-nil → use this param.
	var cc *anthropic.CacheControlEphemeralParam
	if p.promptCaching {
		v := p.cacheControl()
		cc = &v
	}

	msgs, err := toAnthropicMessages(req.Messages, cc)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	model := req.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
	}

	if system := extractSystem(req.System, cc); len(system) > 0 {
		params.System = system
	}

	// Reasoning maps to either an effort level (adaptive thinking) or a
	// token budget (fixed thinking). Anthropic rejects temperature while
	// thinking is enabled, so track whether we applied it.
	thinking := false
	if native, ok := llm.ClampReasoning(model, req.Reasoning); ok {
		if budget, err := strconv.Atoi(native); err == nil {
			// Legacy fixed-budget thinking (numeric value like "8192").
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
			thinking = true
			if params.MaxTokens <= int64(budget) {
				params.MaxTokens = int64(budget) + int64(maxTokens)
			}
		} else {
			// Effort-based adaptive thinking (string like "low"/"medium"/"high").
			adaptive := anthropic.NewThinkingConfigAdaptiveParam()
			params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
			params.OutputConfig = anthropic.OutputConfigParam{
				Effort: anthropic.OutputConfigEffort(native),
			}
			thinking = true
		}
	}
	if req.Temperature != nil && !thinking && llm.Capabilities(model).SupportsTemperature {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = anthropic.Float(*req.TopP)
	}
	if tools := toAnthropicTools(req.Tools, cc); len(tools) > 0 {
		params.Tools = tools
	}

	return params, nil
}

// providerOpts translates the request's provider-specific option bag for
// Anthropic into per-request SDK options. Keys are applied in sorted order for
// deterministic request bodies.
func providerOpts(req llm.Request) []option.RequestOption {
	bag := req.Provider["anthropic"]
	if len(bag) == 0 {
		return nil
	}
	keys := make([]string, 0, len(bag))
	for k := range bag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts := make([]option.RequestOption, 0, len(keys))
	for _, k := range keys {
		opts = append(opts, option.WithJSONSet(k, bag[k]))
	}
	return opts
}

// prepare validates the request and resolves params + per-request options.
func (p *Provider) prepare(ctx context.Context, req llm.Request) (anthropic.MessageNewParams, []option.RequestOption, error) {
	if p.apiKey == "" && p.creds == nil {
		return anthropic.MessageNewParams{}, nil, llm.Registry.New(llm.ErrMissingAPIKey)
	}
	if len(req.Messages) == 0 {
		return anthropic.MessageNewParams{}, nil, llm.Registry.New(llm.ErrEmptyMessages)
	}

	// Resolve auth first so we know whether it's OAuth (for TTL auto-detect).
	authOpts, isOAuth, err := p.authOpts(ctx)
	if err != nil {
		return anthropic.MessageNewParams{}, nil, err
	}

	// Latch cache TTL on first use (env > config > auto: OAuth→1h, API-key→5m).
	if p.promptCaching {
		p.resolveCacheTTL(isOAuth)
	}

	// When thinking is not enabled on this request, strip all thinking blocks
	// from history. The API rejects thinking blocks in messages when the
	// request doesn't have thinking enabled (e.g., reasoning toggled off,
	// subagent without reasoning, or resumed session from a thinking-enabled
	// turn). This must happen before buildParams so toAnthropicMessages
	// never sees them.
	if _, hasReasoning := llm.ClampReasoning(req.Model, req.Reasoning); !hasReasoning {
		req.Messages = stripAllThinkingBlocks(req.Messages)
	}

	params, err := p.buildParams(req)
	if err != nil {
		return anthropic.MessageNewParams{}, nil, err
	}

	if isOAuth {
		params = withBilling(params)
	}

	// Build beta header string — legacy parity.
	betaParts := []string{
		"interleaved-thinking-2025-05-14",
		"thinking-token-count-2026-05-13",
		"context-management-2025-06-27",
		"prompt-caching-scope-2026-01-05",
		"advisor-tool-2026-03-01",
		"advanced-tool-use-2025-11-20",
		"extended-cache-ttl-2025-04-11",
		"cache-diagnosis-2026-04-07",
	}
	// effort beta only when reasoning is active (legacy parity).
	if params.Thinking.OfAdaptive != nil || params.Thinking.OfEnabled != nil {
		betaParts = append(betaParts, "effort-2025-11-24")
	}
	betaParts = append(betaParts, "fine-grained-tool-streaming-2025-05-14")

	// Merge request options.
	opts := append(authOpts, providerOpts(req)...)
	if isOAuth {
		betaStr := oauthBeta + "," + strings.Join(betaParts, ",") +
			",mid-conversation-system-2026-04-07,structured-outputs-2025-12-15"
		// Opus gets 1M context.
		if strings.Contains(strings.ToLower(string(params.Model)), "opus") {
			betaStr += ",context-1m-2025-08-07"
		}
		opts = append(opts, option.WithHeader("anthropic-beta", betaStr))
	} else {
		betaStr := strings.Join(betaParts, ",")
		if strings.Contains(strings.ToLower(string(params.Model)), "opus") {
			betaStr += ",context-1m-2025-08-07"
		}
		opts = append(opts, option.WithHeader("anthropic-beta", betaStr))
	}

	// Inject context_management for thinking-enabled requests.
	opts = append(opts, p.contextManagementOpts(params)...)

	return params, opts, nil
}

// retry401 reports whether the failed call should be retried after the
// credential source refreshed an expired OAuth token.
func (p *Provider) retry401(ctx context.Context, err error) bool {
	return p.creds != nil && isUnauthorized(err) && p.creds.HandleUnauthorized(ctx)
}

// Chat implements llm.Provider.
//
// The SDK's non-streaming Messages.New refuses requests whose estimated
// duration (based on max_tokens) exceeds 10 minutes, requiring streaming
// instead (see CalculateNonStreamingTimeout). Callers that don't need
// incremental deltas (e.g. subagents, compaction) still use Chat, so we pin
// an explicit request timeout here — the SDK skips its duration check
// whenever RequestTimeout is already set — and let the context/model's own
// bounds govern how long we actually wait.
func (p *Provider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	params, opts, err := p.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	opts = append(opts, option.WithRequestTimeout(nonStreamingRequestTimeout))

	message, err := p.client.Messages.New(ctx, params, opts...)
	if err != nil {
		if p.retry401(ctx, err) {
			if params, opts, err = p.prepare(ctx, req); err == nil {
				opts = append(opts, option.WithRequestTimeout(nonStreamingRequestTimeout))
				message, err = p.client.Messages.New(ctx, params, opts...)
			}
		} else if p.isTTLRejection(err) {
			// API rejected 1h TTL — downgrade to 5m and retry.
			p.downgradeCacheTTL()
			if params, opts, err = p.prepare(ctx, req); err == nil {
				opts = append(opts, option.WithRequestTimeout(nonStreamingRequestTimeout))
				message, err = p.client.Messages.New(ctx, params, opts...)
			}
		}
	}
	if err != nil {
		return nil, ParseAnthropicError(err).
			WithDetail("model", string(params.Model))
	}

	p.recordCompletion()
	msg, stopReason, usage := fromAnthropicResponse(message)
	return &llm.Response{Message: msg, StopReason: stopReason, Usage: usage}, nil
}

// ChatStream implements llm.Provider.
func (p *Provider) ChatStream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	params, opts, err := p.prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	stream := p.client.Messages.NewStreaming(ctx, params, opts...)
	if err := stream.Err(); err != nil {
		if p.retry401(ctx, err) {
			stream.Close()
			if params, opts, err = p.prepare(ctx, req); err != nil {
				return nil, err
			}
			stream = p.client.Messages.NewStreaming(ctx, params, opts...)
		} else if p.isTTLRejection(err) {
			stream.Close()
			p.downgradeCacheTTL()
			if params, opts, err = p.prepare(ctx, req); err != nil {
				return nil, err
			}
			stream = p.client.Messages.NewStreaming(ctx, params, opts...)
		}
	}
	return &anthropicStream{stream: stream, provider: p}, nil
}

type sdkStream interface {
	Next() bool
	Current() anthropic.MessageStreamEventUnion
	Err() error
	Close() error
}

type anthropicStream struct {
	stream          sdkStream
	acc             anthropic.Message
	provider        *Provider
	lastThinkingLen int // tracks accumulated thinking chars to compute deltas
}

func (s *anthropicStream) Next() (llm.StreamEvent, error) {
	for s.stream.Next() {
		event := s.stream.Current()

		if err := s.acc.Accumulate(event); err != nil {
			return llm.StreamEvent{}, ParseAnthropicError(err)
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "thinking" {
				s.lastThinkingLen = 0 // reset for new thinking block
			}
			if event.ContentBlock.Type == "tool_use" {
				return llm.StreamEvent{ToolUseStreaming: &llm.ToolUseStreamingEvent{
					ID:   event.ContentBlock.ID,
					Name: event.ContentBlock.Name,
				}}, nil
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				return llm.StreamEvent{TextDelta: event.Delta.Text}, nil
			case "thinking_delta":
				// Adaptive thinking sends empty thinking text with
				// estimated_tokens. Always emit the event so the TUI
				// shows "Thinking deeply..." regardless of content.
				// Try to get actual text from the accumulator (SDK bug
				// in legacy.26.0: event.Delta.Thinking is always empty).
				delta := " " // sentinel so the event always fires
				for i := len(s.acc.Content) - 1; i >= 0; i-- {
					if s.acc.Content[i].Type == "thinking" {
						total := len(s.acc.Content[i].Thinking)
						if total > s.lastThinkingLen {
							delta = s.acc.Content[i].Thinking[s.lastThinkingLen:]
							s.lastThinkingLen = total
						}
						break
					}
				}
				return llm.StreamEvent{ThinkingDelta: delta}, nil
			case "input_json_delta":
				return llm.StreamEvent{InputJSONDeltaLen: len(event.Delta.PartialJSON)}, nil
			}
		case "message_stop":
			if s.provider != nil {
				s.provider.recordCompletion()
			}
			msg, stopReason, usage := fromAnthropicResponse(&s.acc)
			return llm.StreamEvent{
				Done:       true,
				Message:    msg,
				StopReason: stopReason,
				Usage:      usage,
			}, nil
		}
	}

	if err := s.stream.Err(); err != nil {
		return llm.StreamEvent{}, ParseAnthropicError(err)
	}

	// Stream ended without an explicit message_stop; return terminal event.
	msg, stopReason, usage := fromAnthropicResponse(&s.acc)
	return llm.StreamEvent{Done: true, Message: msg, StopReason: stopReason, Usage: usage}, nil
}

func (s *anthropicStream) Close() error {
	return s.stream.Close()
}
