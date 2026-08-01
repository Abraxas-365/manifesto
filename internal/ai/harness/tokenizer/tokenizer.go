// Package tokenizer provides a fast, offline BPE token counter used to estimate
// conversation size for auto-compaction.
//
// It uses the o200k_base BPE (GPT-4o family) as a close, dependency-free proxy
// for Claude's tokenizer — far more accurate than a chars/4 heuristic. The vocab
// is embedded (no network/runtime download), keeping the host a single binary.
//
// The authoritative token count still comes from the API's reported usage; this
// package only fills in the delta of content added since the last response, and
// the cold-start estimate before the first response.
package tokenizer

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

var (
	encOnce sync.Once
	enc     *tiktoken.Tiktoken
)

func encoder() *tiktoken.Tiktoken {
	encOnce.Do(func() {
		// Offline vocab is embedded in tiktoken-go-loader — no runtime download.
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
		e, err := tiktoken.GetEncoding("o200k_base")
		if err != nil {
			return // enc stays nil; callers fall back to the chars/4 heuristic
		}
		enc = e
	})
	return enc
}

// Count returns the BPE token count of s. Falls back to a chars/4 heuristic if
// the encoder could not initialize (it never returns 0 for non-empty input).
func Count(s string) int {
	if s == "" {
		return 0
	}
	if e := encoder(); e != nil {
		return len(e.EncodeOrdinary(s))
	}
	return len(s) / 4
}
