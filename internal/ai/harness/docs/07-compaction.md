# 07 — Compaction (gestión de contexto)

Las conversaciones largas acaban llenando la ventana de contexto del modelo. Un
`Compactor` reduce el historial antes de un turno cuando la estimación de tokens
supera un umbral. Es **opt-in**: sin `Compactor`, el agente nunca compacta.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness`

## La interfaz

```go
type Compactor interface {
    Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error)
}
```

## Cómo se dispara

Antes de cada turno el agente estima los tokens del historial + system. Si supera
`CompactThreshold × ContextWindow`, llama a `Compactor.Compact` y reemplaza el
historial con el resultado. Luego dispara `Hooks.OnCompaction(before, after)`.

Campos relevantes del `Agent` (ver [01](./01-agent.md)):

| Campo | Default | Descripción |
|-------|---------|-------------|
| `Compactor` | `nil` | La estrategia. Nil = desactivado. |
| `CompactThreshold` | `0.8` (`DefaultCompactThreshold`) | Fracción de la ventana a la que dispara. |
| `ContextWindow` | busca por `Model`, si no `200000` | Ventana en tokens. |
| `TokenEstimator` | `EstimateTokens` | Cómo se estiman los tokens. |

```go
func EstimateTokens(msgs []llm.Message, system string) int
```

El estimador por defecto usa una heurística de ~4 caracteres por token. Puedes
sustituirlo con `agent.TokenEstimator` si tienes un tokenizador real.

## Estrategias incluidas

### TruncateCompactor

Descarta los mensajes más antiguos y conserva los `KeepRecent` más recientes.
Nunca deja huérfano un `tool_result` sin su `tool_use`: avanza el punto de corte.
Barato y sin llamadas extra al modelo.

```go
type TruncateCompactor struct {
    KeepRecent int  // 0 usa 6
}
```

```go
agent := harness.New(provider, registry)
agent.Compactor = harness.TruncateCompactor{KeepRecent: 10}
```

### SummarizeCompactor

Reemplaza los mensajes antiguos por un resumen generado por el LLM, conservando
los `KeepRecent` más recientes. Preserva mejor el sentido a cambio de una llamada
extra al provider.

```go
type SummarizeCompactor struct {
    Provider   llm.Provider  // provider CRUDO, no el agente
    Model      string
    MaxTokens  int           // 0 usa 1024
    KeepRecent int           // 0 usa 6
    Prompt     string        // opcional, instrucción de resumen
}
```

> **Importante**: usa un provider crudo (`openai.New(...)`), no el agente, para
> evitar compactación reentrante.

```go
raw := openai.New(os.Getenv("OPENAI_API_KEY"))
agent := harness.New(raw, registry)
agent.Compactor = harness.SummarizeCompactor{
    Provider:   raw,
    Model:      "gpt-4o-mini",
    KeepRecent: 8,
}
```

## Umbral y ventana personalizados

```go
agent.CompactThreshold = 0.6      // compacta antes
agent.ContextWindow = 32000       // modelo con ventana pequeña
agent.Hooks.OnCompaction = func(before, after int) {
    log.Printf("compactado %d → %d tokens", before, after)
}
```

## Escribir tu propia estrategia

Implementa `Compact`. Por ejemplo, conservar solo mensajes de usuario y el último
del asistente, o hacer un resumen jerárquico. Recuerda no separar un
`tool_result` de su `tool_use` (usa el patrón de `safeCut` como referencia).

Ver el ejemplo runnable [`examples/07_compaction`](../examples/07_compaction).
