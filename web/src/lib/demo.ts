import type { AgentEvent } from "./types"

/**
 * Demo driver: replays a scripted agent session with realistic pacing so the
 * UI can be developed and demoed without a running backend.
 */
export async function streamDemo(
  prompt: string,
  onEvent: (ev: AgentEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const sleep = (ms: number) =>
    new Promise<void>((resolve, reject) => {
      const t = setTimeout(resolve, ms)
      signal?.addEventListener("abort", () => {
        clearTimeout(t)
        reject(new DOMException("aborted", "AbortError"))
      })
    })

  const typeOut = async (text: string, type: "text_delta" | "thinking_delta") => {
    const words = text.split(/(?<=\s)/)
    for (let i = 0; i < words.length; i += 3) {
      if (signal?.aborted) throw new DOMException("aborted", "AbortError")
      onEvent({ type, text: words.slice(i, i + 3).join("") })
      await sleep(24)
    }
  }

  try {
    await sleep(300)
    onEvent({ type: "turn", n: 0 })
    await typeOut(
      "Let me look at the project structure first to understand what we're working with.",
      "thinking_delta",
    )

    onEvent({
      type: "tool_start",
      id: "t1",
      name: "Glob",
      input: { pattern: "**/*.go" },
      summary: "**/*.go",
    })
    await sleep(700)
    onEvent({
      type: "tool_end",
      id: "t1",
      ok: true,
      result: "internal/ai/harness/agent.go\ninternal/ai/harness/compact.go\ninternal/ai/harness/llm/message.go\n… 214 files",
    })

    onEvent({
      type: "tool_start",
      id: "t2",
      name: "Read",
      input: { file_path: "internal/ai/harness/agent.go" },
      summary: "internal/ai/harness/agent.go",
    })
    await sleep(600)
    onEvent({ type: "tool_end", id: "t2", ok: true, result: "// 812 lines read" })

    onEvent({
      type: "todo_update",
      todos: [
        { content: "Explore harness package", status: "completed" },
        { content: "Answer the question", status: "in_progress", activeForm: "Answering the question" },
      ],
    })

    onEvent({ type: "turn", n: 1 })
    await typeOut(
      `You asked: **"${prompt.slice(0, 80)}"**\n\nThis is the demo driver — no backend is connected. What you're seeing:\n\n- **Streaming text** rendered incrementally as markdown\n- **Thinking** shown in a collapsible block above\n- **Tool calls** with live status, inputs and results\n- **Todos** tracked in the side panel\n\n\`\`\`go\nagent := harness.New(provider, registry)\nanswer, err := agent.Run(ctx, prompt)\n\`\`\`\n\nWire \`POST /api/v1/agent/chat\` (SSE) on the Go side and the same UI renders the real harness events.`,
      "text_delta",
    )
    onEvent({ type: "text_done" })
    onEvent({
      type: "todo_update",
      todos: [
        { content: "Explore harness package", status: "completed" },
        { content: "Answer the question", status: "completed" },
      ],
    })
    onEvent({ type: "usage", usage: { inputTokens: 14832, outputTokens: 612, contextWindow: 200000 } })
    onEvent({ type: "done" })
  } catch (e) {
    if ((e as Error).name !== "AbortError") throw e
  }
}
