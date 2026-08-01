// Domain types for the agent chat UI. These mirror the harness's event
// surface: streaming text/thinking deltas, tool lifecycle, todos, usage.

export type Role = "user" | "assistant"

export interface ToolCall {
  id: string
  name: string
  input: unknown
  /** Pretty one-line summary of the input (e.g. the bash command). */
  summary?: string
  status: "running" | "ok" | "error" | "pending_approval" | "denied"
  result?: string
  startedAt: number
  endedAt?: number
}

export interface Todo {
  content: string
  status: "pending" | "in_progress" | "completed"
  activeForm?: string
}

/** One renderable item in the conversation, in order. */
export type ChatItem =
  | { kind: "user"; id: string; text: string }
  | { kind: "text"; id: string; text: string; streaming: boolean }
  | { kind: "thinking"; id: string; text: string; streaming: boolean }
  | { kind: "tool"; id: string; call: ToolCall }

export interface Usage {
  inputTokens: number
  outputTokens: number
  contextWindow: number
}

export interface SessionMeta {
  id: string
  title: string
  updatedAt: number
}

/** Server-sent events emitted by the agent endpoint. */
export type AgentEvent =
  | { type: "text_delta"; text: string }
  | { type: "thinking_delta"; text: string }
  | { type: "text_done" }
  | { type: "tool_start"; id: string; name: string; input: unknown; summary?: string }
  | { type: "tool_end"; id: string; ok: boolean; result: string }
  | { type: "tool_approval"; id: string; name: string; input: unknown; summary?: string }
  | { type: "todo_update"; todos: Todo[] }
  | { type: "usage"; usage: Usage }
  | { type: "turn"; n: number }
  | { type: "error"; message: string }
  | { type: "done" }

export type AgentStatus = "idle" | "thinking" | "responding" | "tooling" | "awaiting_approval"
