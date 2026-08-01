package prompts

// OutputStyle represents different verbosity modes (legacy port).
type OutputStyle string

const (
	StyleDefault  OutputStyle = "default"
	StyleVerbose  OutputStyle = "verbose"
	StyleConcise  OutputStyle = "concise"
	StyleBrief    OutputStyle = "brief"
	StyleMarkdown OutputStyle = "markdown"
)

// OutputStyleSection returns the system prompt section for the given output
// style ("" for default/unknown styles).
func OutputStyleSection(style OutputStyle) string {
	switch style {
	case StyleVerbose:
		return `# Output Style: Verbose

Provide detailed explanations and reasoning. Include:
- Step-by-step breakdowns of your approach
- Explanation of why you chose certain approaches over alternatives
- Detailed error analysis when things go wrong
- Code comments explaining non-obvious logic
- Context about how changes relate to the broader codebase`

	case StyleConcise:
		return `# Output Style: Concise

Be direct and minimal. Rules:
- Lead with the action or answer, skip preamble
- One sentence where one sentence will do
- Skip "I'll" and "Let me" prefixes — just do it
- Only explain when the user wouldn't understand without it
- No trailing summaries of what you just did`

	case StyleBrief:
		return `# Output Style: Brief

Ultra-minimal output. Rules:
- Respond in 1-2 sentences maximum unless code output
- Never explain what you're about to do — just do it
- Never summarize what you just did
- Only speak when you need user input or hit a blocker
- Code and tool calls need no accompanying text
- If you can answer with just a tool call, do that`

	case StyleMarkdown:
		return `# Output Style: Markdown

Always format your output as well-structured Markdown. Rules:
- Use headers (##, ###) to organize sections
- Use code blocks with language identifiers for all code
- Use bullet points and numbered lists for structured information
- Use bold and italic for emphasis
- Use tables when comparing or listing structured data
- Include horizontal rules between major sections`

	default:
		return ""
	}
}
