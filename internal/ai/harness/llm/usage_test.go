package llm

import "testing"

func TestUsage_Add(t *testing.T) {
	a := Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}
	b := Usage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40}
	got := a.Add(b)
	want := Usage{InputTokens: 11, OutputTokens: 22, CacheReadTokens: 33, CacheWriteTokens: 44}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
