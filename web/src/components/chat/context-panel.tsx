import { Check, Circle, CircleDashed } from "lucide-react"
import { cn } from "@/lib/utils"
import type { Todo, Usage } from "@/lib/types"

/** Right-hand context panel: agent plan (todos) + context window usage. */
export function ContextPanel({ todos, usage }: { todos: Todo[]; usage: Usage | null }) {
  const pct = usage ? Math.min(100, Math.round(((usage.inputTokens + usage.outputTokens) / usage.contextWindow) * 100)) : 0

  return (
    <aside className="hidden w-72 shrink-0 flex-col gap-6 border-l bg-sidebar p-4 lg:flex">
      <section>
        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Plan</h3>
        {todos.length === 0 ? (
          <p className="text-xs text-muted-foreground">No active plan. The agent posts its task list here.</p>
        ) : (
          <ul className="space-y-1.5">
            {todos.map((t, i) => (
              <li key={i} className="flex items-start gap-2 text-sm">
                {t.status === "completed" ? (
                  <Check className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
                ) : t.status === "in_progress" ? (
                  <CircleDashed className="mt-0.5 size-3.5 shrink-0 animate-spin text-blue-500 [animation-duration:3s]" />
                ) : (
                  <Circle className="mt-0.5 size-3.5 shrink-0 text-muted-foreground/50" />
                )}
                <span
                  className={cn(
                    t.status === "completed" && "text-muted-foreground line-through",
                    t.status === "in_progress" && "font-medium",
                  )}
                >
                  {t.status === "in_progress" && t.activeForm ? t.activeForm : t.content}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      {usage && (
        <section>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Context</h3>
          <div className="mb-1 flex justify-between text-xs text-muted-foreground">
            <span>
              {((usage.inputTokens + usage.outputTokens) / 1000).toFixed(1)}k / {(usage.contextWindow / 1000).toFixed(0)}k tokens
            </span>
            <span>{pct}%</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div
              className={cn("h-full rounded-full transition-all", pct > 80 ? "bg-destructive" : pct > 60 ? "bg-amber-500" : "bg-emerald-500")}
              style={{ width: `${pct}%` }}
            />
          </div>
        </section>
      )}
    </aside>
  )
}
