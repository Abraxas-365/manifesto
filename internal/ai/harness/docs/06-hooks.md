# 06 — Hooks (observabilidad)

Los hooks son callbacks opcionales que se disparan durante `Run`. Sirven para
logging, métricas, UIs en tiempo real o barras de progreso — sin acoplar el
harness a ninguna librería concreta.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness`

## El struct `Hooks`

Cada campo es una `func` nileable: un campo nil es un no-op, así que el struct
vacío no cuesta nada. Fija solo los callbacks que te interesan.

```go
type Hooks struct {
    OnTurnStart        func(turn int)
    OnAssistantText    func(text string)
    OnTextDelta        func(delta string)
    OnThinkingDelta    func(delta string)
    OnToolUseStreaming func(id, name string)
    OnInputJSONDelta   func(deltaLen int)
    OnToolStart        func(id, name string, input json.RawMessage) *ToolIntercept
    OnToolEnd          func(name string, result llm.ContentBlock) *llm.ContentBlock
    OnRetry            func(attempt int, err error, delay time.Duration)
    OnCompactionStart  func()
    OnCompaction       func(before, after int)
    OnCompactionFailed func(err error)
    OnUsage            func(turn int, turnUsage, total llm.Usage)
    OnMicroCompact     func()
}
```

| Callback | Cuándo se dispara |
|----------|-------------------|
| `OnTurnStart` | Al inicio de cada turno del bucle (0-based). |
| `OnAssistantText` | Cuando el modelo devuelve texto final. |
| `OnTextDelta` | Por cada chunk incremental de texto. Fijarlo activa streaming (`ChatStream`). |
| `OnThinkingDelta` | Por cada chunk incremental de razonamiento/thinking. |
| `OnToolUseStreaming` | Cuando un bloque tool_use empieza a streamear (nombre conocido, input aún no). |
| `OnInputJSONDelta` | Por cada chunk de input_json_delta, con su longitud en bytes. |
| `OnToolStart` | Antes de ejecutar una herramienta. Devuelve `*ToolIntercept` para bloquear o modificar la llamada, o nil para permitirla. |
| `OnToolEnd` | Después de ejecutarla, con el bloque de resultado enviado al modelo. Devuelve `*ContentBlock` para reemplazarlo. |
| `OnRetry` | Antes de reintentar una llamada al provider (requiere `EnableRetry`). |
| `OnCompactionStart` | Antes de empezar una compactación (muestra un spinner). |
| `OnCompaction` | Tras compactar el historial con éxito, con tokens estimados antes/después. |
| `OnCompactionFailed` | Cuando un intento de compactación falla (o no logra reducir el historial). El turno continúa con el historial sin compactar. |
| `OnUsage` | Tras cada respuesta del provider, con el uso del turno y el acumulado. |
| `OnMicroCompact` | Tras un micro-compact que limpia tool results viejos. Invalida aquí tus caches (p.ej. `ReadCache`). |

## Interceptar herramientas

```go
type ToolIntercept struct {
    Cancel        bool            // bloquea la llamada
    ErrorMessage  string          // resultado devuelto al modelo si Cancel
    ModifiedInput json.RawMessage // reemplaza el input; nil = original
}
```

## Uso

Asigna el campo `Hooks` del agente:

```go
agent := harness.New(provider, registry)
agent.Hooks = harness.Hooks{
    OnTurnStart: func(turn int) {
        log.Printf("── turno %d ──", turn)
    },
    OnToolStart: func(id, name string, input json.RawMessage) *harness.ToolIntercept {
        log.Printf("→ %s %s", name, input)
        return nil
    },
    OnToolEnd: func(name string, result llm.ContentBlock) *llm.ContentBlock {
        log.Printf("← %s: %s", name, result.Content)
        return nil
    },
    OnAssistantText: func(text string) {
        fmt.Println(text)
    },
    OnUsage: func(turn int, turnUsage, total llm.Usage) {
        log.Printf("tokens turno=%d total=%d",
            turnUsage.OutputTokens, total.OutputTokens)
    },
}
```

## OnRetry

`OnRetry` solo se dispara si activas el retry. `EnableRetry` puentea el callback
del decorador de reintentos a este hook, y lo lee en tiempo de ejecución, así que
el orden no importa:

```go
agent := harness.New(provider, registry).EnableRetry()
agent.Hooks.OnRetry = func(attempt int, err error, delay time.Duration) {
    log.Printf("reintento #%d en %s: %v", attempt, delay, err)
}
```

Ver [05 — Retry](./05-retry-router.md).

## OnCompaction

Solo se dispara si hay un `Compactor` configurado. Reporta la estimación de
tokens antes y después de compactar. Ver [07 — Compaction](./07-compaction.md).

```go
agent.Hooks.OnCompaction = func(before, after int) {
    log.Printf("compactado: %d → %d tokens", before, after)
}
```

Ver el ejemplo runnable [`examples/03_retry_hooks`](../examples/03_retry_hooks).
