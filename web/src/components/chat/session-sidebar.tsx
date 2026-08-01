import { MessageSquarePlus, Moon, Sun, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { SessionMeta } from "@/lib/types"

export function SessionSidebar({
  sessions,
  activeId,
  onSelect,
  onNew,
  onDelete,
  dark,
  onToggleTheme,
}: {
  sessions: SessionMeta[]
  activeId: string
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
  dark: boolean
  onToggleTheme: () => void
}) {
  return (
    <aside className="hidden w-60 shrink-0 flex-col border-r bg-sidebar md:flex">
      <div className="flex items-center gap-2 p-3">
        <div className="flex size-7 items-center justify-center rounded-lg bg-primary font-mono text-xs font-bold text-primary-foreground">
          M
        </div>
        <span className="text-sm font-semibold">Manifesto Agent</span>
      </div>

      <div className="px-3 pb-2">
        <Button variant="outline" size="sm" className="w-full justify-start gap-2" onClick={onNew}>
          <MessageSquarePlus className="size-4" />
          New session
        </Button>
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-2">
        {sessions.map((s) => (
          <div
            key={s.id}
            className={cn(
              "group flex items-center rounded-lg text-sm",
              s.id === activeId ? "bg-sidebar-accent text-sidebar-accent-foreground" : "hover:bg-sidebar-accent/50",
            )}
          >
            <button className="min-w-0 flex-1 truncate px-3 py-2 text-left" onClick={() => onSelect(s.id)}>
              {s.title}
            </button>
            <button
              className="mr-1 hidden rounded p-1 text-muted-foreground hover:text-destructive group-hover:block"
              onClick={() => onDelete(s.id)}
              title="Delete session"
            >
              <Trash2 className="size-3.5" />
            </button>
          </div>
        ))}
      </nav>

      <div className="border-t p-3">
        <Button variant="ghost" size="sm" className="w-full justify-start gap-2" onClick={onToggleTheme}>
          {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
          {dark ? "Light mode" : "Dark mode"}
        </Button>
      </div>
    </aside>
  )
}
