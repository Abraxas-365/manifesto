import type { AgentEvent } from "./types"

/**
 * Transport for the agent chat. Speaks SSE over POST /api/v1/agent/chat.
 * When the backend is not reachable, the caller can fall back to the demo
 * driver (see demo.ts) so the UI is usable standalone.
 */
export async function streamChat(
  sessionId: string,
  prompt: string,
  onEvent: (ev: AgentEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/v1/agent/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, prompt }),
    signal,
  })
  if (!res.ok || !res.body) {
    throw new Error(`agent endpoint: ${res.status} ${res.statusText}`)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ""

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })

    // Parse SSE frames: lines of "data: {...}" separated by blank lines.
    let idx: number
    while ((idx = buf.indexOf("\n\n")) >= 0) {
      const frame = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      for (const line of frame.split("\n")) {
        if (!line.startsWith("data:")) continue
        const payload = line.slice(5).trim()
        if (!payload || payload === "[DONE]") continue
        try {
          onEvent(JSON.parse(payload) as AgentEvent)
        } catch {
          // skip malformed frame
        }
      }
    }
  }
}

export async function approveTool(sessionId: string, toolId: string, approve: boolean): Promise<void> {
  await fetch("/api/v1/agent/approve", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, tool_id: toolId, approve }),
  })
}
