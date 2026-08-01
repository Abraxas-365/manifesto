import { useState } from "react"
import { Brain, ChevronRight } from "lucide-react"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { cn } from "@/lib/utils"

/** Collapsible reasoning block. Collapsed by default once streaming ends. */
export function ThinkingBlock({ text, streaming }: { text: string; streaming: boolean }) {
  const [open, setOpen] = useState(false)
  const expanded = streaming || open

  return (
    <Collapsible open={expanded} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
        <ChevronRight className={cn("size-3 transition-transform", expanded && "rotate-90")} />
        <Brain className="size-3" />
        <span className={cn(streaming && "shimmer")}>{streaming ? "Thinking…" : "Thought process"}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1.5 border-l-2 border-muted pl-3 text-xs italic leading-relaxed text-muted-foreground whitespace-pre-wrap">
          {text}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
