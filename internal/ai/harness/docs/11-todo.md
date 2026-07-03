# 11 — Todo (lista de tareas)

Una herramienta declarativa de lista de tareas para que el agente planifique y
haga seguimiento de trabajo multi-paso. El modelo envía la lista **completa** en
cada llamada y reemplaza la anterior (misma semántica que el `TodoWrite` de la
CLI).

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness/todo`

## Uso mínimo

El valor cero funciona sin configuración: usa un store en memoria interno.

```go
import "github.com/Abraxas-365/manifesto/internal/ai/harness/todo"

registry.Register(&todo.Tool{})
```

Nombre por defecto de la herramienta: `TodoWrite`.

## El struct `Tool`

```go
type Tool struct {
    Store    Store              // nil = store interno en memoria
    ToolName string             // default "TodoWrite"
    Desc     string             // default: descripción declarativa
    OnChange func(items []Item) // opcional: se llama tras reemplazar la lista
}
```

`OnChange` es útil para reflejar la lista en una UI o barra de progreso:

```go
registry.Register(&todo.Tool{
    OnChange: func(items []todo.Item) {
        for _, it := range items {
            fmt.Printf("%s %s\n", it.Status, it.Content)
        }
    },
})
```

## Estructura de un item

```go
type Item struct {
    Content    string `json:"content"`    // imperativo: "Run tests"
    Status     Status `json:"status"`     // pending | in_progress | completed
    ActiveForm string `json:"activeForm"` // presente continuo: "Running tests"
}

const (
    StatusPending    Status = "pending"
    StatusInProgress Status = "in_progress"
    StatusCompleted  Status = "completed"
)
```

Validación en `Execute`: `content` y `activeForm` son obligatorios, `status` debe
ser uno de los tres, y **solo un item** puede estar `in_progress` a la vez.

## Store persistente

El store por defecto es `MemoryStore` (seguro para concurrencia, valor cero listo
para usar). Para persistir entre procesos o compartir con una UI, implementa
`Store`:

```go
type Store interface {
    Get(ctx context.Context) ([]Item, error)
    Set(ctx context.Context, items []Item) error
}
```

```go
registry.Register(&todo.Tool{Store: myRedisStore})
```

Las implementaciones deben ser seguras para uso concurrente.

## Inspeccionar la lista

Fuera de la llamada del modelo puedes leer el estado actual:

```go
func (t *Tool) Items(ctx context.Context) ([]Item, error)
```

```go
items, _ := todoTool.Items(ctx)
```

## Cuándo usarlo

- Tareas de 3+ pasos donde quieres que el agente planifique y muestre progreso.
- Sáltalo en tareas triviales de un solo paso: añade ruido sin beneficio.

Ver el ejemplo runnable [`examples/10_todo`](../examples/10_todo).
