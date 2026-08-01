import { useState } from "react"
import {
  CheckCircle2,
  ChevronRight,
  CircleAlert,
  Loader2,
  ShieldQuestion,
  Terminal,
  FileText,
  FilePen,
  Search,
  FolderSearch,
  ListTodo,
  Bot,
  Wrench,
} from "lucide-react"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { ToolCall } from "@/lib/types"

const TOOL_ICONS: Record<string, typeof Terminal> = {
  Bash: Terminal,
  Read: FileText,
  Write: FilePen,
  Edit: FilePen,
  Grep: Search,
  Glob: FolderSearch,
  List: FolderSearch,
  TodoWrite: ListTodo,
  Task: Bot,
}

function inputSummary(call: ToolCall): string {
  if (call.summary) return call.summary
  if (call.input == null) return ""
  if (typeof call.input === "string") return call.input
  const obj = call.input as Record<string, unknown>
  const key = ["command", "file_path", "pattern", "prompt", "query"].find((k) => typeof obj[k] === "string")
  if (key) return obj[key] as string
  const s = JSON.stringify(call.input)
  return s.length > 120 ? s.slice(0, 120) + "…" : s
}

/**
 * One tool invocation: status icon + name + input summary on a single row,
 * expandable to show the full input and result. Approval requests render
 * Allow / Deny actions inline.
 */
export function ToolCallCard({
  call,
  onApprove,
}: {
  call: ToolCall
  onApprove?: (id: string, approve: boolean) => void
}) {
  const [open, setOpen] = useState(false)
  const Icon = TOOL_ICONS[call.name] ?? Wrench
  const summary = inputSummary(call)

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div
        className={cn(
          "rounded-lg border bg-card text-card-foreground",
          call.status === "pending_approval" && "border-amber-500/50",
          call.status === "error" && "border-destructive/40",
        )}
      >
        <CollapsibleTrigger className="flex w-full items-center gap-2 px-3 py-2 text-left">
          <ChevronRight
            className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")}
          />
          <Icon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="text-xs font-medium">{call.name}</span>
          {summary && (
            <code
              className={cn(
                "min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground",
                call.status === "running" && "shimmer",
              )}
            >
              {summary}
            </code>
          )}
          <span className="ml-auto shrink-0">
            {call.status === "running" && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
            {call.status === "ok" && <CheckCircle2 className="size-3.5 text-emerald-500" />}
            {call.status === "error" && <CircleAlert className="size-3.5 text-destructive" />}
            {call.status === "pending_approval" && <ShieldQuestion className="size-3.5 text-amber-500" />}
            {call.status === "denied" && <CircleAlert className="size-3.5 text-muted-foreground" />}
          </span>
        </CollapsibleTrigger>

        {call.status === "pending_approval" && onApprove && (
          <div className="flex items-center gap-2 border-t px-3 py-2">
            <span className="text-xs text-muted-foreground">This action requires approval</span>
            <div className="ml-auto flex gap-2">
              <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => onApprove(call.id, false)}>
                Deny
              </Button>
              <Button size="sm" className="h-7 text-xs" onClick={() => onApprove(call.id, true)}>
                Allow
              </Button>
            </div>
          </div>
        )}

        <CollapsibleContent>
          <div className="space-y-2 border-t px-3 py-2">
            <div>
              <div className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Input</div>
              <pre className="max-h-48 overflow-auto rounded bg-muted/50 p-2 font-mono text-xs">
                {typeof call.input === "string" ? call.input : JSON.stringify(call.input, null, 2)}
              </pre>
            </div>
            {call.result != null && (
              <div>
                <div className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                  Result
                </div>
                <pre className="max-h-64 overflow-auto rounded bg-muted/50 p-2 font-mono text-xs whitespace-pre-wrap">
                  {call.result}
                </pre>
              </div>
            )}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}
