package memoryx

import "github.com/Abraxas-365/manifesto/pkg/ai/llm"

// TokenEstimator estimates token counts for messages.
// The default implementation uses a simple heuristic (1 token ≈ 4 chars).
// Provide a custom implementation for more accurate counting (e.g. tiktoken).
type TokenEstimator interface {
	EstimateTokens(messages []llm.Message) int
}

// CharBasedEstimator estimates tokens using a characters-per-token ratio.
// This is a rough approximation — good enough for triggering summarization
// thresholds, but not for exact billing.
type CharBasedEstimator struct {
	CharsPerToken int // defaults to 4 if zero
}

func (e *CharBasedEstimator) ratio() int {
	if e.CharsPerToken <= 0 {
		return 4
	}
	return e.CharsPerToken
}

func (e *CharBasedEstimator) EstimateTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		// Each message has ~4 tokens of overhead (role, separators)
		total += 4
		total += len(m.Content) / e.ratio()
		if m.Name != "" {
			total += len(m.Name) / e.ratio()
		}
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Name) / e.ratio()
			total += len(tc.Function.Arguments) / e.ratio()
		}
	}
	return total
}
