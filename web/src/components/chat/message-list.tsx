import { useEffect, useRef, useState } from "react"
import { ArrowDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Markdown } from "./markdown"
import { ThinkingBlock } from "./thinking-block"
import { ToolCallCard } from "./tool-call"
import type { AgentStatus, ChatItem } from "@/lib/types"

/**
 * Message list with agentic-chat scroll behavior: auto-follow while streaming,
 * disengage when the user scrolls up, and a jump-to-bottom affordance.
 */
export function MessageList({
  items,
  status,
  onApprove,
}: {
  items: ChatItem[]
  status: AgentStatus
  onApprove?: (id: string, approve: boolean) => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [follow, setFollow] = useState(true)

  useEffect(() => {
    if (!follow) return
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [items, follow])

  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
    setFollow(atBottom)
  }

  return (
    <div className="relative min-h-0 flex-1">
      <div ref={scrollRef} onScroll={onScroll} className="h-full overflow-y-auto">
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-6">
          {items.length === 0 && (
            <div className="flex flex-col items-center gap-2 py-24 text-center">
              <div className="text-2xl font-semibold">What are we building today?</div>
              <p className="max-w-md text-sm text-muted-foreground">
                Ask the agent to explore the codebase, run commands, edit files, or research a question.
              </p>
            </div>
          )}

          {items.map((item) => {
            switch (item.kind) {
              case "user":
                return (
                  <div key={item.id} className="flex justify-end">
                    <div className="max-w-[85%] rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground whitespace-pre-wrap">
                      {item.text}
                    </div>
                  </div>
                )
              case "text":
                return (
                  <div key={item.id} className="max-w-none">
                    <Markdown>{item.text}</Markdown>
                    {item.streaming && <span className="ml-0.5 inline-block h-4 w-2 animate-pulse bg-foreground/70 align-text-bottom" />}
                  </div>
                )
              case "thinking":
                return <ThinkingBlock key={item.id} text={item.text} streaming={item.streaming} />
              case "tool":
                return <ToolCallCard key={item.id} call={item.call} onApprove={onApprove} />
            }
          })}

          {status === "thinking" && items[items.length - 1]?.kind === "user" && (
            <div className="shimmer text-sm">Thinking…</div>
          )}
        </div>
      </div>

      {!follow && (
        <Button
          size="icon"
          variant="outline"
          className="absolute bottom-4 left-1/2 size-8 -translate-x-1/2 rounded-full shadow-md"
          onClick={() => {
            setFollow(true)
            const el = scrollRef.current
            if (el) el.scrollTop = el.scrollHeight
          }}
        >
          <ArrowDown className="size-4" />
        </Button>
      )}
    </div>
  )
}
