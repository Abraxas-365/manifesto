// Package toolsearch provides a meta-tool that lets the model load the full
// schemas of deferred tools on demand. Deferred tools are advertised to the
// model as name + hint only (via a system-reminder); calling ToolSearch reveals
// their schema and makes them visible for the rest of the run.
package toolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// ToolName is the name exposed to the model.
const ToolName = "ToolSearch"

// Tool implements tool.Tool. It resolves deferred tools from Registry and marks
// them discovered in Discovery.
type Tool struct {
	Registry  *tool.Registry
	Discovery *tool.Discovery
}

type input struct {
	Query      string       `json:"query"`
	MaxResults tool.FlexInt `json:"max_results"`
}

func (t *Tool) Name() string { return ToolName }

func (t *Tool) Description() string {
	return `Fetches full schema definitions for deferred tools so they can be called.

Deferred tools appear by name in <system-reminder> messages. Until fetched, only the name is known — there is no parameter schema, so the tool cannot be invoked. This tool takes a query, matches it against the deferred tool list, and returns the matched tools' complete JSONSchema definitions inside a <functions> block. Once a tool's schema appears in that result, it is callable exactly like any tool defined at the top of the prompt.

Query forms:
- "select:Read,Edit,Grep" — fetch these exact tools by name
- "notebook jupyter" — keyword search, up to max_results best matches`
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Query to find deferred tools. Use \"select:<tool_name>\" for direct selection, or keywords to search."},
			"max_results": {"type": "number", "description": "Maximum number of results to return (default: 5)", "default": 5}
		},
		"required": ["query"]
	}`)
}

func (t *Tool) IsReadOnly() bool { return true }

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if t.Registry == nil {
		return &tool.Result{Content: "ToolSearch not initialized: no registry", IsError: true}, nil
	}
	maxResults := in.MaxResults.Value(5)
	if maxResults <= 0 {
		maxResults = 5
	}

	hints := t.Registry.SearchHints() // only deferred tools are discoverable
	query := strings.TrimSpace(in.Query)

	var matched []string
	if rest, ok := strings.CutPrefix(query, "select:"); ok {
		for _, name := range strings.Split(rest, ",") {
			name = strings.TrimSpace(name)
			if _, ok := hints[name]; ok {
				matched = append(matched, name)
			}
		}
	} else {
		matched = t.keywordMatch(query, hints, maxResults)
	}

	if len(matched) == 0 {
		return &tool.Result{Content: "No tools matched the query."}, nil
	}

	var b strings.Builder
	b.WriteString("<functions>\n")
	for _, name := range matched {
		if t.Discovery != nil {
			t.Discovery.Reveal(name)
		}
		tl, ok := t.Registry.Get(name)
		if !ok {
			continue
		}
		def := struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		}{tl.Name(), tl.Description(), tl.InputSchema()}
		defJSON, _ := json.Marshal(def)
		fmt.Fprintf(&b, "<function>%s</function>\n", defJSON)
	}
	b.WriteString("</functions>")
	return &tool.Result{Content: b.String()}, nil
}

// keywordMatch scores deferred tools by name (x2) and hint (x1) against the
// query keywords, returning up to maxResults names sorted by score descending.
func (t *Tool) keywordMatch(query string, hints map[string]string, maxResults int) []string {
	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score int
	}
	var candidates []scored
	for name, hint := range hints {
		lname := strings.ToLower(name)
		lhint := strings.ToLower(hint)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lname, kw) {
				score += 2
			}
			if strings.Contains(lhint, kw) {
				score++
			}
		}
		if score > 0 {
			candidates = append(candidates, scored{name, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].name < candidates[j].name
	})
	out := make([]string, 0, maxResults)
	for i, c := range candidates {
		if i >= maxResults {
			break
		}
		out = append(out, c.name)
	}
	return out
}
