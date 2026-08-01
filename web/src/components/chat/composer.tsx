import { useRef, useState, type KeyboardEvent } from "react"
import { ArrowUp, Square } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { AgentStatus } from "@/lib/types"

/** Composer: auto-growing textarea, Enter to send, Esc/button to stop. */
export function Composer({
  status,
  onSend,
  onStop,
}: {
  status: AgentStatus
  onSend: (text: string) => void
  onStop: () => void
}) {
  const [value, setValue] = useState("")
  const taRef = useRef<HTMLTextAreaElement>(null)
  const busy = status !== "idle"

  const submit = () => {
    if (!value.trim() || busy) return
    onSend(value)
    setValue("")
    if (taRef.current) taRef.current.style.height = "auto"
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
    if (e.key === "Escape" && busy) onStop()
  }

  return (
    <div className="border-t bg-background">
      <div className="mx-auto max-w-3xl px-4 py-3">
        <div className="flex items-end gap-2 rounded-2xl border bg-card p-2 shadow-sm focus-within:ring-1 focus-within:ring-ring">
          <textarea
            ref={taRef}
            value={value}
            onChange={(e) => {
              setValue(e.target.value)
              e.target.style.height = "auto"
              e.target.style.height = Math.min(e.target.scrollHeight, 200) + "px"
            }}
            onKeyDown={onKeyDown}
            placeholder="Message the agent… (Enter to send, Shift+Enter for newline)"
            rows={1}
            className="max-h-[200px] min-h-9 flex-1 resize-none bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground"
          />
          {busy ? (
            <Button size="icon" variant="destructive" className="size-9 shrink-0 rounded-xl" onClick={onStop} title="Stop (Esc)">
              <Square className="size-4" />
            </Button>
          ) : (
            <Button size="icon" className="size-9 shrink-0 rounded-xl" onClick={submit} disabled={!value.trim()} title="Send (Enter)">
              <ArrowUp className="size-4" />
            </Button>
          )}
        </div>
        <div className="mt-1.5 px-2 text-center text-[10px] text-muted-foreground">
          The agent can read files, run commands, and edit code. Review actions before approving.
        </div>
      </div>
    </div>
  )
}
