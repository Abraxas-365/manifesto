import { useCallback, useEffect, useMemo, useState } from "react"
import { MessageList } from "@/components/chat/message-list"
import { Composer } from "@/components/chat/composer"
import { ContextPanel } from "@/components/chat/context-panel"
import { SessionSidebar } from "@/components/chat/session-sidebar"
import { useAgentChat } from "@/hooks/use-agent-chat"
import { approveTool } from "@/lib/api"
import type { SessionMeta } from "@/lib/types"

const newSession = (): SessionMeta => ({
  id: crypto.randomUUID(),
  title: "New session",
  updatedAt: Date.now(),
})

export default function App() {
  const [sessions, setSessions] = useState<SessionMeta[]>(() => [newSession()])
  const [activeId, setActiveId] = useState(() => sessions[0].id)
  const [dark, setDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches)

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark)
  }, [dark])

  const chat = useAgentChat(activeId)

  const handleSend = useCallback(
    (text: string) => {
      chat.send(text)
      // First message titles the session.
      setSessions((prev) =>
        prev.map((s) =>
          s.id === activeId && s.title === "New session"
            ? { ...s, title: text.slice(0, 40) + (text.length > 40 ? "…" : ""), updatedAt: Date.now() }
            : s.id === activeId
              ? { ...s, updatedAt: Date.now() }
              : s,
        ),
      )
    },
    [chat, activeId],
  )

  const handleNew = useCallback(() => {
    const s = newSession()
    setSessions((prev) => [s, ...prev])
    setActiveId(s.id)
    chat.reset()
  }, [chat])

  const handleDelete = useCallback(
    (id: string) => {
      setSessions((prev) => {
        const rest = prev.filter((s) => s.id !== id)
        if (rest.length === 0) {
          const s = newSession()
          setActiveId(s.id)
          return [s]
        }
        if (id === activeId) setActiveId(rest[0].id)
        return rest
      })
      if (id === activeId) chat.reset()
    },
    [activeId, chat],
  )

  const handleApprove = useCallback(
    (toolId: string, approve: boolean) => {
      void approveTool(activeId, toolId, approve)
    },
    [activeId],
  )

  const sorted = useMemo(() => [...sessions].sort((a, b) => b.updatedAt - a.updatedAt), [sessions])

  return (
    <div className="flex h-screen bg-background text-foreground">
      <SessionSidebar
        sessions={sorted}
        activeId={activeId}
        onSelect={setActiveId}
        onNew={handleNew}
        onDelete={handleDelete}
        dark={dark}
        onToggleTheme={() => setDark((d) => !d)}
      />

      <main className="flex min-w-0 flex-1 flex-col">
        <MessageList items={chat.items} status={chat.status} onApprove={handleApprove} />
        <Composer status={chat.status} onSend={handleSend} onStop={chat.stop} />
      </main>

      <ContextPanel todos={chat.todos} usage={chat.usage} />
    </div>
  )
}
