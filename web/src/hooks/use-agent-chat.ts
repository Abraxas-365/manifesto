import { useCallback, useRef, useState } from "react"
import type { AgentEvent, AgentStatus, ChatItem, Todo, ToolCall, Usage } from "@/lib/types"
import { streamChat } from "@/lib/api"
import { streamDemo } from "@/lib/demo"

let nextId = 0
const uid = () => `i${++nextId}`

export interface AgentChat {
  items: ChatItem[]
  todos: Todo[]
  usage: Usage | null
  status: AgentStatus
  send: (prompt: string) => void
  stop: () => void
  reset: () => void
}

/**
 * useAgentChat owns the conversation state machine: it consumes the SSE
 * event stream and folds it into an ordered list of renderable items.
 * Tries the real backend first; falls back to the demo driver when the
 * endpoint is unreachable.
 */
export function useAgentChat(sessionId: string): AgentChat {
  const [items, setItems] = useState<ChatItem[]>([])
  const [todos, setTodos] = useState<Todo[]>([])
  const [usage, setUsage] = useState<Usage | null>(null)
  const [status, setStatus] = useState<AgentStatus>("idle")
  const abortRef = useRef<AbortController | null>(null)

  const handleEvent = useCallback((ev: AgentEvent) => {
    switch (ev.type) {
      case "text_delta":
        setStatus("responding")
        setItems((prev) => {
          const last = prev[prev.length - 1]
          if (last?.kind === "text" && last.streaming) {
            return [...prev.slice(0, -1), { ...last, text: last.text + ev.text }]
          }
          return [...prev, { kind: "text", id: uid(), text: ev.text, streaming: true }]
        })
        break
      case "thinking_delta":
        setStatus("thinking")
        setItems((prev) => {
          const last = prev[prev.length - 1]
          if (last?.kind === "thinking" && last.streaming) {
            return [...prev.slice(0, -1), { ...last, text: last.text + ev.text }]
          }
          return [...prev, { kind: "thinking", id: uid(), text: ev.text, streaming: true }]
        })
        break
      case "text_done":
        setItems((prev) =>
          prev.map((it) =>
            (it.kind === "text" || it.kind === "thinking") && it.streaming
              ? { ...it, streaming: false }
              : it,
          ),
        )
        break
      case "tool_start": {
        setStatus("tooling")
        const call: ToolCall = {
          id: ev.id,
          name: ev.name,
          input: ev.input,
          summary: ev.summary,
          status: "running",
          startedAt: Date.now(),
        }
        setItems((prev) => [
          // close out any streaming text/thinking block first
          ...prev.map((it) =>
            (it.kind === "text" || it.kind === "thinking") && it.streaming
              ? { ...it, streaming: false }
              : it,
          ),
          { kind: "tool", id: ev.id, call },
        ])
        break
      }
      case "tool_end":
        setItems((prev) =>
          prev.map((it) =>
            it.kind === "tool" && it.call.id === ev.id
              ? {
                  ...it,
                  call: {
                    ...it.call,
                    status: ev.ok ? "ok" : "error",
                    result: ev.result,
                    endedAt: Date.now(),
                  },
                }
              : it,
          ),
        )
        break
      case "tool_approval": {
        setStatus("awaiting_approval")
        const call: ToolCall = {
          id: ev.id,
          name: ev.name,
          input: ev.input,
          summary: ev.summary,
          status: "pending_approval",
          startedAt: Date.now(),
        }
        setItems((prev) => [...prev, { kind: "tool", id: ev.id, call }])
        break
      }
      case "todo_update":
        setTodos(ev.todos)
        break
      case "usage":
        setUsage(ev.usage)
        break
      case "error":
        setItems((prev) => [
          ...prev,
          { kind: "text", id: uid(), text: `**Error:** ${ev.message}`, streaming: false },
        ])
        break
      case "done":
        setStatus("idle")
        break
    }
  }, [])

  const send = useCallback(
    (prompt: string) => {
      const trimmed = prompt.trim()
      if (!trimmed) return
      abortRef.current?.abort()
      const ac = new AbortController()
      abortRef.current = ac

      setItems((prev) => [...prev, { kind: "user", id: uid(), text: trimmed }])
      setStatus("thinking")

      streamChat(sessionId, trimmed, handleEvent, ac.signal)
        .catch((err: Error) => {
          if (err.name === "AbortError") return
          // Backend unreachable → demo mode.
          return streamDemo(trimmed, handleEvent, ac.signal)
        })
        .finally(() => {
          if (!ac.signal.aborted) setStatus("idle")
        })
    },
    [sessionId, handleEvent],
  )

  const stop = useCallback(() => {
    abortRef.current?.abort()
    setStatus("idle")
    setItems((prev) =>
      prev.map((it) =>
        (it.kind === "text" || it.kind === "thinking") && it.streaming
          ? { ...it, streaming: false }
          : it,
      ),
    )
  }, [])

  const reset = useCallback(() => {
    abortRef.current?.abort()
    setItems([])
    setTodos([])
    setUsage(null)
    setStatus("idle")
  }, [])

  return { items, todos, usage, status, send, stop, reset }
}
